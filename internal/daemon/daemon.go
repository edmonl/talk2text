// Package daemon runs the talk2text recorder and transcription daemon.
package daemon

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/edmonl/talk2text/internal/audiocapture"
	"github.com/edmonl/talk2text/internal/daemon/config"
	"github.com/edmonl/talk2text/internal/daemon/notifier"
	"github.com/edmonl/talk2text/internal/daemon/session"
	"github.com/edmonl/talk2text/internal/runtimedir"
	"github.com/edmonl/talk2text/internal/util"
	"github.com/edmonl/talk2text/internal/util/timer"
	"github.com/edmonl/talk2text/internal/whisper"
)

type daemon struct {
	// cfg is set during construction and must not be mutated while the daemon runs.
	cfg       *config.Config
	log       *log.Logger
	ctx       context.Context
	cancel    context.CancelFunc
	muCapture sync.Mutex

	whisper *whisper.Client
	notify  *notifier.Notifier

	newAudio                 audiocapture.Factory
	stream                   audiocapture.Stream
	warmTimer                timer.Timer
	desiredStreamState       streamState
	desiredStreamStateSignal chan struct{}
	streamManagerDone        chan struct{}

	active          *session.Session
	nextClip        int
	stopTimer       timer.Timer
	pendingStopClip int

	ongoingTranscriptions atomic.Int32
	transcriptionIdle     chan struct{}
	httpAdmitted          atomic.Int32

	muTranscripts                    sync.Mutex
	transcriptRetentionHighWatermark int
	protectedTranscripts             map[int]struct{}
}

// Run runs the long-lived daemon until ctx is canceled.
func Run(ctx context.Context, cfg config.Config, stderr io.Writer) error {
	logger := log.New(stderr, "talk2text: ", 0)
	if err := runtimedir.PrepareDir(cfg.RuntimeDir, logger); err != nil {
		return err
	}
	socketPath, err := runtimedir.PrepareSocket(cfg.RuntimeDir)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	d := &daemon{
		cfg:    &cfg,
		log:    logger,
		ctx:    ctx,
		cancel: cancel,

		whisper: whisper.NewClient(whisper.Config{
			Endpoint:       cfg.WhisperEndpoint,
			ConnectTimeout: cfg.WhisperConnectTimeout,
			RequestTimeout: cfg.WhisperRequestTimeout,
		}),
		notify: notifier.New(ctx, cfg.NotifyCmd, logger),

		newAudio:                 audiocapture.NewMalgoStream,
		desiredStreamStateSignal: make(chan struct{}, 1),
		streamManagerDone:        make(chan struct{}),

		nextClip: 1,

		transcriptionIdle: make(chan struct{}, 1),
	}
	if cfg.TranscriptRetentionWindow > 0 {
		d.protectedTranscripts = make(map[int]struct{})
	}
	if d.cfg.WarmRetention > 0 {
		d.warmTimer = timer.NewCallbackTimer(d.cfg.WarmRetention, func() {
			d.muCapture.Lock()
			defer d.muCapture.Unlock()
			if d.active == nil {
				d.desireStream(streamOff)
			}
		})
	} else {
		d.warmTimer = timer.NewImmediateTimer(func() {
			d.desireStream(streamOff)
		})
	}
	go d.streamManager()

	err = d.serve(socketPath)
	d.shutdown()
	logger.Print("daemon stopped")
	return err
}

func (d *daemon) start(errChan chan error) {
	d.muCapture.Lock()
	close(errChan)

	d.warmTimer.Stop()
	var stopped *session.Session
	if d.active != nil && d.active.ID() == d.pendingStopClip {
		stopped = d.active
	}
	d.cancelPendingStop()
	clipID := d.nextClip
	d.nextClip++
	d.active = session.NewSession(clipID)
	d.desireStream(streamRecording)
	d.muCapture.Unlock()

	if stopped != nil {
		d.processStoppedSession(stopped)
	}
}

func (d *daemon) stop(errChan chan error) {
	d.muCapture.Lock()
	close(errChan)

	if d.active == nil {
		d.muCapture.Unlock()
		return
	}

	if d.cfg.StopDelay > 0 {
		clipID := d.active.ID()
		if d.pendingStopClip == clipID {
			// repeated stop
			d.muCapture.Unlock()
			return
		}

		if d.pendingStopClip == 0 {
			d.pendingStopClip = clipID
			d.stopTimer = timer.NewCallbackTimer(d.cfg.StopDelay, func() {
				d.muCapture.Lock()
				if d.active == nil || d.active.ID() != clipID || d.pendingStopClip != clipID {
					d.muCapture.Unlock()
					return
				}
				s := d.stopActiveSession()
				d.muCapture.Unlock()
				d.processStoppedSession(s)
			})
			d.stopTimer.Start()
			d.muCapture.Unlock()
			return
		}
	}

	s := d.stopActiveSession()
	d.muCapture.Unlock()
	d.processStoppedSession(s)
}

func (d *daemon) shutdown() {
	d.muCapture.Lock()
	d.warmTimer.Stop()
	d.cancelPendingStop()
	d.active = nil
	d.desireStream(streamOff)
	d.muCapture.Unlock()

	select {
	case <-d.streamManagerDone:
	case <-time.After(time.Second):
		d.log.Printf("audio capture did not stop promptly")
		return
	}
}

func (d *daemon) onAudio(pcm []byte) {
	if len(pcm) == 0 {
		return
	}

	d.muCapture.Lock()
	if d.active == nil {
		d.muCapture.Unlock()
		return
	}

	s := d.active
	wasEmpty := s.Duration() == 0
	if err := s.OnPCM(pcm); err != nil {
		// Recording may be ongoing and have the error while the stop is delayed.
		d.cancelPendingStop()
		d.active = nil
		d.desireStream(streamWarm)
		d.muCapture.Unlock()
		d.log.Printf("failed to write PCM bytes of clip %d: %v", s.ID(), err)
		d.notify.Error("audio-capture", fmt.Sprintf("Failed to record clip %d", s.ID()))
		return
	}
	if d.cfg.MaxDuration > 0 && s.Duration() >= d.cfg.MaxDuration {
		// Recording may be ongoing and reach the limit while the stop is delayed.
		d.cancelPendingStop()
		d.active = nil
		d.desireStream(streamWarm)
		d.muCapture.Unlock()
		if wasEmpty {
			d.notify.Info("record-start", fmt.Sprintf("Recording clip %d", s.ID()))
		}
		d.notify.Info("record-stop", fmt.Sprintf("Recording clip %d stopped with max duration", s.ID()))
		go d.transcribe(s)
		return
	}
	d.muCapture.Unlock()
	if wasEmpty {
		d.notify.Info("record-start", fmt.Sprintf("Recording clip %d", s.ID()))
	}
}

func (d *daemon) notifyErr(err error, code, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	d.notify.Error(code, util.UpperFirst(msg))
	d.log.Printf(msg+": %v", err)
}

// stopActiveSession detaches the active session and transitions the stream out
// of recording. The caller must hold d.muCapture and ensure d.active is not nil.
func (d *daemon) stopActiveSession() *session.Session {
	s := d.active
	d.active = nil
	d.cancelPendingStop()
	d.desireStream(streamWarm)
	d.warmTimer.Start()
	return s
}

// cancelPendingStop invalidates any delayed stop. The caller must hold
// d.muCapture.
func (d *daemon) cancelPendingStop() {
	if d.stopTimer != nil {
		d.stopTimer.Stop()
		d.stopTimer = nil
	}
	d.pendingStopClip = 0
}

func (d *daemon) processStoppedSession(s *session.Session) {
	if s.Duration() > 0 {
		d.notify.Info("record-stop", fmt.Sprintf("Recording clip %d stopped", s.ID()))
	}
	d.transcribe(s)
}
