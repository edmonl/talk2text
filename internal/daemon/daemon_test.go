package daemon

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/edmonl/talk2text/internal/audiocapture"
	"github.com/edmonl/talk2text/internal/daemon/config"
	"github.com/edmonl/talk2text/internal/daemon/notifier"
	"github.com/edmonl/talk2text/internal/daemon/session"
	"github.com/edmonl/talk2text/internal/runtimedir"
	"github.com/edmonl/talk2text/internal/util/timer"
	"github.com/edmonl/talk2text/internal/whisper"
)

type fakeAudioStream struct {
	onPCM    func([]byte)
	startErr error
	started  atomic.Bool
	stopped  atomic.Bool
	closed   atomic.Bool
}

func (f *fakeAudioStream) Start() error {
	f.started.Store(true)
	return f.startErr
}

func (f *fakeAudioStream) Stop() error {
	f.stopped.Store(true)
	return nil
}

func (f *fakeAudioStream) Close() error {
	f.closed.Store(true)
	return nil
}

type fakeStreamHolder struct {
	stream atomic.Pointer[fakeAudioStream]
}

func (h *fakeStreamHolder) Load() *fakeAudioStream {
	return h.stream.Load()
}

func fakeFactory(holder *fakeStreamHolder) audiocapture.Factory {
	return func(cfg audiocapture.Config) (audiocapture.Stream, error) {
		stream := &fakeAudioStream{onPCM: cfg.OnPCM}
		holder.stream.Store(stream)
		return stream, nil
	}
}

func fakeFactoryError(err error) audiocapture.Factory {
	return func(audiocapture.Config) (audiocapture.Stream, error) {
		return nil, err
	}
}

func TestDaemonClassifiesShortClipByPCMDuration(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(run, "out.log")
	outCmd := filepath.Join(run, "out")
	script := "#!/bin/sh\nprintf '%s\\t%s\\t%s\\n' \"$1\" \"$2\" \"$(cat \"$2\")\" >> " + shellQuote(logPath) + "\n"
	if err := os.WriteFile(outCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stream fakeStreamHolder
	var stderr bytes.Buffer
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeDir = run
	cfg.MinDuration = 10 * time.Millisecond
	cfg.WarmRetention = time.Millisecond
	cfg.OutCmd = outCmd
	logger := log.New(&stderr, "talk2text: ", 0)
	d := &daemon{
		cfg:      &cfg,
		log:      logger,
		newAudio: fakeFactory(&stream),
		whisper: whisper.NewClient(whisper.Config{
			Endpoint:       cfg.WhisperEndpoint,
			ConnectTimeout: cfg.WhisperConnectTimeout,
			RequestTimeout: cfg.WhisperRequestTimeout,
		}),
		notify:   notifier.New(context.Background(), "", logger),
		nextClip: 1,
		ctx:      context.Background(),
	}
	startStreamManager(t, d)
	d.warmTimer = newTestWarmTimer(d)
	startDaemon(t, d)
	waitForStartedStream(t, &stream).onPCM([]byte{0, 0, 1, 0})
	stopDaemon(t, d)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(logPath); err == nil && strings.Contains(string(raw), "short\t") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	t.Fatalf("short output not observed; log=%q stderr=%q stream=%#v", string(raw), stderr.String(), stream.Load())
}

func TestDaemonStopsStreamBeforeWarmRetentionAndClosesAfter(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	var stream fakeStreamHolder
	var stderr bytes.Buffer
	cfg, err := config.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeDir = run
	cfg.WarmRetention = 50 * time.Millisecond
	logger := log.New(&stderr, "talk2text: ", 0)
	d := &daemon{
		cfg:      &cfg,
		log:      logger,
		newAudio: fakeFactory(&stream),
		whisper: whisper.NewClient(whisper.Config{
			Endpoint:       cfg.WhisperEndpoint,
			ConnectTimeout: cfg.WhisperConnectTimeout,
			RequestTimeout: cfg.WhisperRequestTimeout,
		}),
		notify:   notifier.New(context.Background(), "", logger),
		nextClip: 1,
		ctx:      context.Background(),
	}
	startStreamManager(t, d)
	d.warmTimer = newTestWarmTimer(d)
	startDaemon(t, d)
	startedStream := waitForStartedStream(t, &stream)
	stopDaemon(t, d)
	waitForCondition(t, time.Second, func() bool {
		return startedStream.stopped.Load()
	}, "stream was not stopped when recording stopped")
	if startedStream.closed.Load() {
		t.Fatal("stream closed before warm retention elapsed")
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if startedStream.closed.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stream was not closed after warm retention; stderr=%q", stderr.String())
}

func TestDaemonStartOpenStreamErrorDoesNotLeaveMutexLocked(t *testing.T) {
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{}
	d := &daemon{
		cfg:      &cfg,
		log:      logger,
		newAudio: fakeFactoryError(errors.New("open failed")),
		notify:   notifier.New(context.Background(), "", logger),
		nextClip: 1,
		ctx:      context.Background(),
	}
	startStreamManager(t, d)
	d.warmTimer = timer.NewCallbackTimer(time.Hour, func() {})

	errChan := make(chan error, 1)
	d.start(errChan)
	if _, ok := <-errChan; ok {
		t.Fatal("start error channel was not closed")
	}
	waitForNoActiveSession(t, d, "active session was kept after open stream failure")
	if !d.muCapture.TryLock() {
		t.Fatal("capture mutex remained locked after open stream failure")
	}
	d.muCapture.Unlock()
}

func TestDaemonStartStreamStartErrorDoesNotLeaveMutexLocked(t *testing.T) {
	var stream fakeStreamHolder
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{}
	d := &daemon{
		cfg:      &cfg,
		log:      logger,
		newAudio: fakeFactory(&stream),
		notify:   notifier.New(context.Background(), "", logger),
		nextClip: 1,
		ctx:      context.Background(),
	}
	startStreamManager(t, d)
	d.warmTimer = timer.NewCallbackTimer(time.Hour, func() {})

	d.stream = &fakeAudioStream{startErr: errors.New("start failed")}
	errChan := make(chan error, 1)
	d.start(errChan)
	if _, ok := <-errChan; ok {
		t.Fatal("start error channel was not closed")
	}
	waitForNoActiveSession(t, d, "active session was kept after stream start failure")
	if !d.muCapture.TryLock() {
		t.Fatal("capture mutex remained locked after stream start failure")
	}
	d.muCapture.Unlock()
}

func TestDaemonShutdownWithoutStreamDoesNotPanic(t *testing.T) {
	d := &daemon{
		warmTimer: timer.NewCallbackTimer(time.Hour, func() {}),
	}
	cancel := startStreamManager(t, d)
	cancel()
	d.shutdown()
}

func TestProcessTranscriptKeepsFileWhenOutputCommandFails(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	outCmd := filepath.Join(run, "out")
	if err := os.WriteFile(outCmd, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir: run,
		OutCmd:     outCmd,
	}
	d := &daemon{
		cfg:    &cfg,
		log:    logger,
		notify: notifier.New(context.Background(), "", logger),
		ctx:    context.Background(),
	}

	d.processTranscript(42, "hello world", true)

	path := filepath.Join(run, "transcripts", "42.txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript was not kept after output command failure: %v", err)
	}
	if string(raw) != "hello world" {
		t.Fatalf("transcript = %q, want original text", string(raw))
	}
}

func TestProcessTranscriptAllowsOutputCommandToRemoveFile(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	outCmd := filepath.Join(run, "out")
	if err := os.WriteFile(outCmd, []byte("#!/bin/sh\nrm -f -- \"$2\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir: run,
		OutCmd:     outCmd,
	}
	d := &daemon{
		cfg:    &cfg,
		log:    logger,
		notify: notifier.New(context.Background(), "", logger),
		ctx:    context.Background(),
	}

	d.processTranscript(42, "hello world", true)

	path := filepath.Join(run, "transcripts", "42.txt")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("transcript still exists or stat failed differently: %v", err)
	}
	if strings.Contains(stderr.String(), "failed to remove transcript") {
		t.Fatalf("daemon logged missing transcript after output command removed it:\n%s", stderr.String())
	}
}

func TestTranscribeDoesNotReportWhisperErrorWhenContextCanceled(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()

	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		RuntimeDir: run,
	}
	d := &daemon{
		cfg: &cfg,
		log: logger,
		whisper: whisper.NewClient(whisper.Config{
			Endpoint: server.URL,
		}),
		notify: notifier.New(ctx, "", logger),
		ctx:    ctx,
	}
	s := session.NewSession(7)
	if err := s.OnPCM(bytes.Repeat([]byte{0, 0}, 320)); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		d.transcribe(s)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("test Whisper server was not called")
	}
	cancel()
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("transcribe did not return after context cancellation")
	}
	if d.pending.Load() != 0 {
		t.Fatalf("pending transcriptions = %d, want 0", d.pending.Load())
	}
	if strings.Contains(stderr.String(), "whisper server failed") {
		t.Fatalf("canceled transcription was logged as Whisper failure:\n%s", stderr.String())
	}
}

func TestProcessTranscriptDoesNotStartOutputCommandAfterContextCanceled(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(run, "out-ran")
	outCmd := filepath.Join(run, "out")
	script := "#!/bin/sh\nprintf ran > " + shellQuote(marker) + "\n"
	if err := os.WriteFile(outCmd, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir: run,
		OutCmd:     outCmd,
	}
	d := &daemon{
		cfg:    &cfg,
		log:    logger,
		notify: notifier.New(ctx, "", logger),
		ctx:    ctx,
	}

	d.processTranscript(42, "hello world", true)

	transcriptPath := filepath.Join(run, "transcripts", "42.txt")
	raw, err := os.ReadFile(transcriptPath)
	if err != nil {
		t.Fatalf("transcript was not kept after canceled context: %v", err)
	}
	if string(raw) != "hello world" {
		t.Fatalf("transcript = %q, want original text", string(raw))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("output command ran after context cancellation, stat err = %v", err)
	}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func newTestWarmTimer(d *daemon) timer.Timer {
	return timer.NewCallbackTimer(d.cfg.WarmRetention, func() {
		d.muCapture.Lock()
		defer d.muCapture.Unlock()
		if d.active == nil {
			d.desireStream(streamOff)
		}
	})
}

func startStreamManager(t *testing.T, d *daemon) context.CancelFunc {
	t.Helper()
	if d.desiredStreamStateSignal == nil {
		d.desiredStreamStateSignal = make(chan struct{}, 1)
	}
	if d.streamManagerDone == nil {
		d.streamManagerDone = make(chan struct{})
	}
	base := d.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	d.ctx = ctx
	go d.streamManager()
	t.Cleanup(func() {
		cancel()
		select {
		case <-d.streamManagerDone:
		case <-time.After(time.Second):
			t.Fatal("stream manager did not stop")
		}
	})
	return cancel
}

func waitForStartedStream(t *testing.T, holder *fakeStreamHolder) *fakeAudioStream {
	t.Helper()
	var stream *fakeAudioStream
	waitForCondition(t, time.Second, func() bool {
		stream = holder.Load()
		return stream != nil && stream.started.Load()
	}, "stream was not started")
	return stream
}

func waitForNoActiveSession(t *testing.T, d *daemon, message string) {
	t.Helper()
	waitForCondition(t, time.Second, func() bool {
		d.muCapture.Lock()
		defer d.muCapture.Unlock()
		return d.active == nil
	}, message)
}

func waitForCondition(t *testing.T, timeout time.Duration, ok func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

func startDaemon(t *testing.T, d *daemon) {
	t.Helper()
	errChan := make(chan error, 1)
	d.start(errChan)
	if err := <-errChan; err != nil {
		t.Fatalf("start failed: %v", err)
	}
}

func stopDaemon(t *testing.T, d *daemon) {
	t.Helper()
	errChan := make(chan error, 1)
	d.stop(errChan)
	if err := <-errChan; err != nil {
		t.Fatalf("stop failed: %v", err)
	}
}
