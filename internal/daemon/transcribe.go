package daemon

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/edmonl/talk2text/internal/daemon/session"
	"github.com/edmonl/talk2text/internal/runtimedir"
)

func (d *daemon) transcribe(s *session.Session) {
	clipID := s.ID()
	if s.Duration() <= 0 || d.cfg.MinDuration > 0 && s.Duration() < d.cfg.MinDuration {
		d.processTranscript(clipID, "", false)
		return
	}

	d.notify.Info("transcribe-start", fmt.Sprintf("Transcribing clip %d", clipID))
	d.pending.Add(1)
	text, err := d.whisper.Transcribe(d.ctx, clipID, s.PCM(), d.cfg.RuntimeDir)
	d.pending.Add(-1)
	if err != nil {
		if d.ctx.Err() != nil {
			return
		}
		d.log.Printf("whisper server failed to transcribe clip %d: %s", clipID, err)
		d.notify.Error("whisper", fmt.Sprintf("Transcribing clip %d failed", clipID))
		return
	}
	d.notify.Info("transcribe-stop", fmt.Sprintf("Finished transcript %d", clipID))
	if strings.ToLower(text) == "[blank_audio]" {
		text = ""
	}
	d.processTranscript(clipID, text, true)
}

func (d *daemon) processTranscript(clipID int, text string, transcribed bool) {
	outCmd := d.cfg.OutCmd
	var path string
	var err error

	if text != "" || outCmd != "" {
		d.protectTranscript(clipID)
		path, err = runtimedir.WriteTranscript(d.cfg.RuntimeDir, clipID, text)
		if err != nil {
			d.unprotectTranscript(clipID)
			d.log.Printf("failed to write transcript %d: %s", clipID, err)
			d.notify.Error("runtime", fmt.Sprintf("Output transcript %d failed", clipID))
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
	cmd := exec.Command(outCmd, kind, path)
	cmd.Stderr = d.log.Writer()
	if d.ctx.Err() != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		d.log.Printf("failed to start output command for transcript %d: %s", clipID, err)
		d.notify.Error("output-command", fmt.Sprintf("Processing transcript %d failed", clipID))
		return
	}
	d.notify.Info("output-start", fmt.Sprintf("Processing transcript %d", clipID))
	if err := cmd.Wait(); err != nil {
		d.notify.Error("output-command", fmt.Sprintf("Processing transcript %d failed", clipID))
	}
}
