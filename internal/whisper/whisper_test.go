package whisper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientTranscribe(t *testing.T) {
	var sawPrompt bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("temperature") != "0" || r.FormValue("temperature_inc") != "0.9" || r.FormValue("response_format") != "json" {
			t.Fatalf("missing expected whisper form fields: %#v", r.Form)
		}
		if r.FormValue("prompt") == "context text" {
			sawPrompt = true
		}
		files := r.MultipartForm.File["file"]
		if len(files) != 1 {
			t.Fatalf("file parts = %d, want 1", len(files))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":" hello   world "}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "transcription-prompt"), []byte("context text"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := NewClient(Config{
		Endpoint:       server.URL,
		RequestTimeout: time.Second,
	})
	text, err := client.Transcribe(context.Background(), 7, []byte{0, 0, 1, 0}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Fatalf("text = %q, want cleaned text", text)
	}
	if !sawPrompt {
		t.Fatalf("server did not receive cleaned prompt")
	}
}

func TestClientTranscribeUsesContext(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer server.Close()
	defer close(release)

	dir := t.TempDir()
	client := NewClient(Config{Endpoint: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := client.Transcribe(ctx, 7, []byte{0, 0, 1, 0}, dir)
		done <- err
	}()

	<-started
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("Transcribe succeeded")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("err = %v, want context canceled", err)
	}
}

func TestClientTranscribeCapsErrorResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("a", maxErrorBodyBytes) + "tail"))
	}))
	defer server.Close()

	client := NewClient(Config{Endpoint: server.URL})
	_, err := client.Transcribe(context.Background(), 7, []byte{0, 0, 1, 0}, t.TempDir())
	if err == nil {
		t.Fatal("Transcribe succeeded")
	}
	if strings.Contains(err.Error(), "tail") {
		t.Fatalf("err includes uncapped response body tail: %v", err)
	}
	if !strings.Contains(err.Error(), strings.Repeat("a", maxErrorBodyBytes)) {
		t.Fatalf("err = %v, want capped response body", err)
	}
}
