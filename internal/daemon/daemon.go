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
	"github.com/edmonl/talk2text/internal/server"
	"github.com/edmonl/talk2text/internal/util"
	"github.com/edmonl/talk2text/internal/util/timer"
	"github.com/edmonl/talk2text/internal/whisper"
)

type daemon struct {
	// cfg is set during construction and must not be mutated while the daemon runs.
	cfg       *config.Config
	log       *log.Logger
	ctx       context.Context
	muCapture sync.Mutex

	whisper *whisper.Client
	notify  *notifier.Notifier

	newAudio                 audiocapture.Factory
	stream                   audiocapture.Stream
	warmTimer                timer.Timer
	desiredStreamState       streamState
	desiredStreamStateSignal chan struct{}
	streamManagerDone        chan struct{}

	active   *session.Session
	nextClip int
	pending  atomic.Int32

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

	d := &daemon{
		cfg: &cfg,
		log: logger,
		ctx: ctx,

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
	}
	if cfg.TranscriptRetentionWindow > 0 {
		d.protectedTranscripts = make(map[int]struct{})
	}
	d.warmTimer = timer.NewCallbackTimer(d.cfg.WarmRetention, func() {
		d.muCapture.Lock()
		defer d.muCapture.Unlock()
		if d.active == nil {
			d.desireStream(streamOff)
		}
	})
	go d.streamManager()

	logger.Printf("daemon starting to listen on %s", socketPath)
	if err := server.Serve(ctx, socketPath, d); err != nil {
		return fmt.Errorf("failed to start daemon on socket %s: %w", socketPath, err)
	}

	d.shutdown()
	logger.Print("daemon stopped")
	return nil
}

func (d *daemon) start(errChan chan error) {
	d.muCapture.Lock()
	defer d.muCapture.Unlock()
	close(errChan)

	d.warmTimer.Stop()
	clipID := d.nextClip
	d.nextClip++
	d.active = session.NewSession(clipID)
	d.desireStream(streamRecording)
}

func (d *daemon) stop(errChan chan error) {
	d.muCapture.Lock()
	close(errChan)

	if d.active == nil {
		d.muCapture.Unlock()
		return
	}

	s := d.active
	d.active = nil
	if d.cfg.WarmRetention <= 0 {
		d.desireStream(streamOff)
	} else {
		d.desireStream(streamWarm)
		d.warmTimer.Start()
	}
	d.muCapture.Unlock()

	if s != nil {
		if s.Duration() > 0 {
			d.notify.Info("record-stop", fmt.Sprintf("Recording clip %d stopped", s.ID()))
		}
		d.transcribe(s)
	}
}

func (d *daemon) shutdown() {
	d.muCapture.Lock()
	d.warmTimer.Stop()
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
		d.active = nil
		d.desireStream(streamWarm)
		d.muCapture.Unlock()
		d.log.Printf("failed to write PCM bytes of clip %d: %s", s.ID(), err)
		d.notify.Error("audio-capture", fmt.Sprintf("Failed to record clip %d", s.ID()))
		return
	}
	if d.cfg.MaxDuration > 0 && s.Duration() >= d.cfg.MaxDuration {
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
	d.log.Printf(msg+": %s", err)
}
