package daemon

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/edmonl/talk2text/internal/runtimedir"
)

func (d *daemon) protectTranscript(clipID int) {
	if d.protectedTranscripts == nil {
		return
	}

	d.muTranscripts.Lock()
	d.protectedTranscripts[clipID] = struct{}{}
	d.muTranscripts.Unlock()
}

func (d *daemon) unprotectTranscript(clipID int) {
	if d.protectedTranscripts == nil {
		return
	}

	d.muTranscripts.Lock()
	delete(d.protectedTranscripts, clipID)
	d.muTranscripts.Unlock()
}

func (d *daemon) releaseTranscript(clipID int) {
	if d.protectedTranscripts == nil {
		return
	}

	d.muTranscripts.Lock()
	delete(d.protectedTranscripts, clipID)
	d.transcriptRetentionHighWatermark = max(clipID, d.transcriptRetentionHighWatermark)
	files := d.getTranscriptNamesToPrune()
	d.muTranscripts.Unlock()

	transcriptsDir := runtimedir.TranscriptsDir(d.cfg.RuntimeDir)
	for _, name := range files {
		if err := os.Remove(filepath.Join(transcriptsDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			d.log.Printf("failed to prune transcript file %s: %v", name, err)
		}
	}
}

func (d *daemon) getTranscriptNamesToPrune() []string {
	cutoff := d.transcriptRetentionHighWatermark - d.cfg.TranscriptRetentionWindow
	if cutoff < 1 {
		return nil
	}

	files := make([]string, 0)
	err := runtimedir.ProcessTranscriptFiles(d.cfg.RuntimeDir, func(name string, clipID int) {
		if clipID > cutoff {
			return
		}
		if _, ok := d.protectedTranscripts[clipID]; ok {
			return
		}

		files = append(files, name)
	})
	if err != nil {
		d.log.Printf("failed to prune transcripts: %v", err)
	}

	return files
}
