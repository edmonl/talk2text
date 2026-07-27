package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/edmonl/talk2text/internal/daemon/session"
	"github.com/edmonl/talk2text/internal/runtimedir"
)

const (
	outputKindEnv = "TALK2TEXT_OUTPUT_KIND"
	notifyCmdEnv  = "TALK2TEXT_NOTIFY_CMD"
)

func (d *daemon) isShortSession(s *session.Session) bool {
	return s.Duration() <= 0 || d.cfg.MinDuration > 0 && s.Duration() < d.cfg.MinDuration
}

func (d *daemon) transcribe(s *session.Session) {
	if d.isShortSession(s) {
		d.processTranscript(s, "", false)
		return
	}
	d.ongoingTranscriptions.Add(1)
	d.processLongSession(s)
}

func (d *daemon) processLongSession(s *session.Session) {
	clipID := s.ID()

	d.notify.Info("transcribe-start", fmt.Sprintf("Transcribing clip %d", clipID), s.Environment())
	text, err := d.whisper.Transcribe(d.ctx, clipID, s.PCM(), d.cfg.RuntimeDir)
	if d.ongoingTranscriptions.Add(-1) <= 0 {
		select {
		case d.transcriptionIdle <- struct{}{}:
		default:
		}
	}

	if err != nil {
		if d.ctx.Err() != nil {
			return
		}
		d.log.Printf("whisper server failed to transcribe clip %d: %v", clipID, err)
		d.notify.Error("whisper", fmt.Sprintf("Transcribing clip %d failed", clipID), s.Environment())
		return
	}
	d.notify.Info("transcribe-stop", fmt.Sprintf("Finished transcript %d", clipID), s.Environment())
	if strings.ToLower(text) == "[blank_audio]" {
		text = ""
	}
	d.processTranscript(s, text, true)
}

func (d *daemon) processTranscript(s *session.Session, text string, transcribed bool) {
	clipID := s.ID()
	environment := s.Environment()
	outCmd := d.cfg.OutCmd
	var path string
	var err error

	if text != "" || outCmd != "" {
		d.protectTranscript(clipID)
		path, err = runtimedir.WriteTranscript(d.cfg.RuntimeDir, clipID, text)
		if err != nil {
			d.unprotectTranscript(clipID)
			d.log.Printf("failed to write transcript %d: %v", clipID, err)
			d.notify.Error("runtime", fmt.Sprintf("Output transcript %d failed", clipID), environment)
			return
		}
		defer d.releaseTranscript(clipID)
	}

	if outCmd == "" {
		return
	}

	kind := "text"
	if text == "" {
		if transcribed {
			kind = "blank"
		} else {
			kind = "short"
		}
	}
	cmd := exec.Command(outCmd, path)
	cmd.Env = append(append(os.Environ(), environment...),
		outputKindEnv+"="+kind,
		notifyCmdEnv+"="+d.cfg.NotifyCmd,
	)
	cmd.Stderr = d.log.Writer()
	if d.ctx.Err() != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		d.log.Printf("failed to start output command for transcript %d: %v", clipID, err)
		d.notify.Error("output-command", fmt.Sprintf("Processing transcript %d failed", clipID), environment)
		return
	}
	d.notify.Info("output-start", fmt.Sprintf("Processing transcript %d", clipID), environment)
	cmd.Wait()
}
