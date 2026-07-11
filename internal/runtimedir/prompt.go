package runtimedir

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/edmonl/talk2text/internal/util"
)

const promptFileName = "transcription-prompt"

// ReadPrompt returns the trimmed content of the transcription prompt file inside runtimeDir.
func ReadPrompt(runtimeDir string) (string, error) {
	prompt, err := os.ReadFile(filepath.Join(runtimeDir, promptFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return util.CollapseSpace(string(prompt)), nil
}
