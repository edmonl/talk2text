package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edmonl/talk2text/internal/amrwb"
	"github.com/edmonl/talk2text/internal/daemon/config"
	"github.com/edmonl/talk2text/internal/daemon/notifier"
	"github.com/edmonl/talk2text/internal/daemon/session"
	"github.com/edmonl/talk2text/internal/runtimedir"
	"github.com/edmonl/talk2text/internal/whisper"
)

func TestHTTPAudioLimits(t *testing.T) {
	tests := []struct {
		duration   time.Duration
		wantFrames int
		wantBytes  int64
	}{
		{duration: 19 * time.Millisecond, wantFrames: 0, wantBytes: int64(len(amrwb.Magic))},
		{duration: 20 * time.Millisecond, wantFrames: 1, wantBytes: int64(len(amrwb.Magic) + amrwbMaxEncodedFrameSize)},
	}
	for _, tt := range tests {
		frames, bytes := httpAudioLimits(tt.duration)
		if frames != tt.wantFrames || bytes != tt.wantBytes {
			t.Errorf("httpAudioLimits(%v) = %d, %d; want %d, %d", tt.duration, frames, bytes, tt.wantFrames, tt.wantBytes)
		}
	}
}

func TestHTTPRejectsInvalidRequests(t *testing.T) {
	d, _ := newHTTPTestDaemon(t, nil)
	d.cfg.MaxDuration = 20 * time.Millisecond

	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        []byte
		streaming   bool
		wantStatus  int
		wantError   string
	}{
		{
			name:       "unknown path",
			path:       "/unknown",
			wantStatus: http.StatusNotFound,
			wantError:  "not found",
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			wantStatus: http.StatusMethodNotAllowed,
			wantError:  "method not allowed",
		},
		{
			name:        "wrong content type",
			contentType: "audio/wav",
			wantStatus:  http.StatusUnsupportedMediaType,
			wantError:   "content type must be audio/amr-wb",
		},
		{
			name:       "invalid AMR-WB",
			body:       []byte("not AMR-WB"),
			wantStatus: http.StatusBadRequest,
			wantError:  amrwb.ErrInvalidHeader.Error(),
		},
		{
			name:       "too many frames",
			body:       amrwbBody(2),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantError:  amrwb.ErrTooLong.Error() + ": limit is 1 frames",
		},
		{
			name:       "body too large",
			body:       bytes.Repeat([]byte{0}, len(amrwb.Magic)+amrwbMaxEncodedFrameSize+1),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantError:  "body too large",
		},
		{
			name:       "streaming body too large",
			body:       bytes.Repeat([]byte{0}, len(amrwb.Magic)+amrwbMaxEncodedFrameSize+1),
			streaming:  true,
			wantStatus: http.StatusRequestEntityTooLarge,
			wantError:  "body too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodPost
			}
			path := tt.path
			if path == "" {
				path = "/transcribe"
			}
			contentType := tt.contentType
			if contentType == "" {
				contentType = "audio/amr-wb"
			}
			req := httptest.NewRequest(method, path, bytes.NewReader(tt.body))
			req.Header.Set("Content-Type", contentType)
			if tt.streaming {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			d.handleHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var response httpResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Ok || response.Error != tt.wantError {
				t.Fatalf("response = %#v, want error %q", response, tt.wantError)
			}
			if d.httpAdmitted.Load() != 0 {
				t.Fatalf("admitted requests = %d, want 0", d.httpAdmitted.Load())
			}
		})
	}
}

func TestHTTPAcceptedResponseUsesSharedClipSequence(t *testing.T) {
	transcribed := make(chan struct{})
	d, _ := newHTTPTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"text":"hello"}`)
		close(transcribed)
	}))
	d.nextClip = 42
	d.active = session.NewSession(7)

	rec := submitHTTPAudio(d, amrwbBody(1))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var response httpResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Ok || response.ClipID != 42 {
		t.Fatalf("response = %#v", response)
	}

	d.muCapture.Lock()
	nextClip := d.nextClip
	active := d.active
	d.muCapture.Unlock()
	if nextClip != 43 || active == nil || active.ID() != 7 {
		t.Fatalf("next clip = %d, active = %#v", nextClip, active)
	}

	select {
	case <-transcribed:
	case <-time.After(time.Second):
		t.Fatal("accepted clip was not transcribed")
	}
	waitForAtomicValue(t, &d.ongoingTranscriptions, 0)
}

func TestHTTPRejectsBusyRequestWithoutReadingBody(t *testing.T) {
	d, _ := newHTTPTestDaemon(t, nil)
	d.cfg.MinDuration = time.Second
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for range maxHTTPAdmitted {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/transcribe", &gatedBody{gate: gate})
			req.Header.Set("Content-Type", "audio/amr-wb")
			rec := httptest.NewRecorder()
			d.handleHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
			}
		}()
	}
	waitForAtomicValue(t, &d.httpAdmitted, maxHTTPAdmitted)

	body := &countingBody{}
	req := httptest.NewRequest(http.MethodPost, "/transcribe", body)
	req.Header.Set("Content-Type", "audio/amr-wb")
	rec := httptest.NewRecorder()
	d.handleHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if body.reads.Load() != 0 {
		t.Fatalf("busy request body was read %d times", body.reads.Load())
	}

	close(gate)
	wg.Wait()
	waitForAtomicValue(t, &d.httpAdmitted, 0)
}

func TestHTTPTranscriptionsAreSerialized(t *testing.T) {
	called := make(chan int, 2)
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	d, _ := newHTTPTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(calls.Add(1))
		called <- call
		if call == 1 {
			<-releaseFirst
		}
		io.WriteString(w, `{"text":""}`)
	}))

	if rec := submitHTTPAudio(d, amrwbBody(1)); rec.Code != http.StatusAccepted {
		t.Fatalf("first status = %d; body=%s", rec.Code, rec.Body.String())
	}
	waitForCall(t, called, 1)
	if rec := submitHTTPAudio(d, amrwbBody(1)); rec.Code != http.StatusAccepted {
		t.Fatalf("second status = %d; body=%s", rec.Code, rec.Body.String())
	}
	select {
	case call := <-called:
		t.Fatalf("second transcription started early as call %d", call)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirst)
	waitForCall(t, called, 2)
	waitForAtomicValue(t, &d.httpAdmitted, 0)
	waitForAtomicValue(t, &d.ongoingTranscriptions, 0)
}

func TestHTTPWaitsForExistingLocalTranscription(t *testing.T) {
	called := make(chan int, 2)
	releaseLocal := make(chan struct{})
	var calls atomic.Int32
	d, _ := newHTTPTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(calls.Add(1))
		called <- call
		if call == 1 {
			<-releaseLocal
		}
		io.WriteString(w, `{"text":""}`)
	}))

	local := session.NewSession(99)
	if err := local.OnPCM(bytes.Repeat([]byte{0}, 640)); err != nil {
		t.Fatal(err)
	}
	localDone := make(chan struct{})
	go func() {
		d.transcribe(local)
		close(localDone)
	}()
	waitForCall(t, called, 1)

	// Leave a stale notification to verify that the counter, rather than the
	// notification itself, controls whether HTTP may proceed.
	d.transcriptionIdle <- struct{}{}

	if rec := submitHTTPAudio(d, amrwbBody(1)); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	select {
	case call := <-called:
		t.Fatalf("HTTP transcription started while local transcription was active as call %d", call)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseLocal)
	waitForCall(t, called, 2)
	select {
	case <-localDone:
	case <-time.After(time.Second):
		t.Fatal("local transcription did not finish")
	}
	waitForAtomicValue(t, &d.httpAdmitted, 0)
	waitForAtomicValue(t, &d.ongoingTranscriptions, 0)
}

func TestLocalTranscriptionCanStartDuringHTTPTranscription(t *testing.T) {
	called := make(chan int, 2)
	releaseHTTP := make(chan struct{})
	var calls atomic.Int32
	d, _ := newHTTPTestDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := int(calls.Add(1))
		called <- call
		if call == 1 {
			<-releaseHTTP
		}
		io.WriteString(w, `{"text":""}`)
	}))

	if rec := submitHTTPAudio(d, amrwbBody(1)); rec.Code != http.StatusAccepted {
		t.Fatalf("HTTP status = %d; body=%s", rec.Code, rec.Body.String())
	}
	waitForCall(t, called, 1)

	local := session.NewSession(99)
	if err := local.OnPCM(bytes.Repeat([]byte{0}, 640)); err != nil {
		t.Fatal(err)
	}
	localDone := make(chan struct{})
	go func() {
		d.transcribe(local)
		close(localDone)
	}()
	waitForCall(t, called, 2)

	close(releaseHTTP)
	select {
	case <-localDone:
	case <-time.After(time.Second):
		t.Fatal("local transcription did not finish")
	}
	waitForAtomicValue(t, &d.ongoingTranscriptions, 0)
}

func TestHTTPListenerServesAndStopsWithContext(t *testing.T) {
	d, _ := newHTTPTestDaemon(t, nil)
	d.cfg.HTTPListen = "127.0.0.1:0"
	d.cfg.MinDuration = time.Second
	logLines := make(chan string, 1)
	d.log = log.New(channelWriter{lines: logLines}, "", 0)
	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	done := make(chan error, 1)
	go func() {
		done <- d.serveHTTP()
	}()

	var address string
	select {
	case line := <-logLines:
		address = strings.TrimPrefix(strings.TrimSpace(line), "daemon starting to listen on ")
	case <-time.After(time.Second):
		t.Fatal("HTTP listener did not start")
	}
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP listener did not stop")
	}
}

func newHTTPTestDaemon(t *testing.T, whisperHandler http.Handler) (*daemon, *httptest.Server) {
	t.Helper()
	run := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	if err := runtimedir.PrepareDir(run, logger); err != nil {
		t.Fatal(err)
	}
	if whisperHandler == nil {
		whisperHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"text":""}`)
		})
	}
	server := httptest.NewServer(whisperHandler)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg := config.Config{
		RuntimeDir:  run,
		MinDuration: 0,
		MaxDuration: 100 * time.Millisecond,
	}
	d := &daemon{
		cfg:               &cfg,
		log:               logger,
		ctx:               ctx,
		nextClip:          1,
		transcriptionIdle: make(chan struct{}, 1),
		whisper: whisper.NewClient(whisper.Config{
			Endpoint: server.URL,
		}),
		notify: notifier.New(ctx, "", logger),
	}
	return d, server
}

func submitHTTPAudio(d *daemon, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", "audio/amr-wb")
	rec := httptest.NewRecorder()
	d.handleHTTP(rec, req)
	return rec
}

func amrwbBody(frameCount int) []byte {
	body := append([]byte(nil), amrwb.Magic...)
	return append(body, bytes.Repeat([]byte{0x7c}, frameCount)...)
}

type gatedBody struct {
	gate <-chan struct{}
	sent bool
}

func (b *gatedBody) Read(p []byte) (int, error) {
	if b.sent {
		return 0, io.EOF
	}
	<-b.gate
	b.sent = true
	return copy(p, amrwb.Magic), nil
}

type countingBody struct {
	reads atomic.Int32
}

func (b *countingBody) Read([]byte) (int, error) {
	b.reads.Add(1)
	return 0, io.EOF
}

type channelWriter struct {
	lines chan<- string
}

func (w channelWriter) Write(p []byte) (int, error) {
	w.lines <- string(p)
	return len(p), nil
}

func waitForAtomicValue(t *testing.T, value *atomic.Int32, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if value.Load() == int32(want) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("atomic value = %d, want %d", value.Load(), want)
}

func waitForCall(t *testing.T, calls <-chan int, want int) {
	t.Helper()
	select {
	case got := <-calls:
		if got != want {
			t.Fatalf("call = %d, want %d", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("call %d did not start", want)
	}
}
