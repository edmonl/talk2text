package runtimedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPrompt(t *testing.T) {
	dir := t.TempDir()
	prompt, err := ReadPrompt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "" {
		t.Fatalf("missing prompt = %q, want empty", prompt)
	}
	if err := os.WriteFile(filepath.Join(dir, promptFileName), []byte(" hello\n\nworld\t "), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err = ReadPrompt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "hello world" {
		t.Fatalf("prompt = %q, want collapsed prompt", prompt)
	}
}
