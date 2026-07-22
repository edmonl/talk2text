package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func TestDaemonZeroWarmRetentionStopsWithoutDeadlock(t *testing.T) {
	cfg := config.Config{WarmRetention: 0}
	d := &daemon{
		cfg:    &cfg,
		active: session.NewSession(1),
	}
	d.warmTimer = newTestWarmTimer(d)

	done := make(chan struct{})
	go func() {
		errChan := make(chan error, 1)
		d.stop(errChan)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop deadlocked with zero warm retention")
	}

	d.muCapture.Lock()
	defer d.muCapture.Unlock()
	if d.desiredStreamState != streamOff {
		t.Fatalf("desired stream state = %v, want off", d.desiredStreamState)
	}
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
	if err := os.WriteFile(outCmd, []byte("#!/bin/sh\nprintf 'output failed with details\\n' >&2\nexit 7\n"), 0o700); err != nil {
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
	if !strings.Contains(stderr.String(), "output failed with details") {
		t.Fatalf("output command stderr was not forwarded: %q", stderr.String())
	}
}

func TestProcessTranscriptDoesNotRemoveFileWhenOutputCommandSucceeds(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	outCmd := filepath.Join(run, "out")
	if err := os.WriteFile(outCmd, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
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
		t.Fatalf("daemon removed transcript after output command succeeded: %v", err)
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

func TestProcessTranscriptPrunesOutsideWindowWithoutOutputCommand(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir:                run,
		TranscriptRetentionWindow: 2,
	}
	d := &daemon{
		cfg:                  &cfg,
		log:                  logger,
		notify:               notifier.New(context.Background(), "", logger),
		ctx:                  context.Background(),
		protectedTranscripts: make(map[int]struct{}),
	}

	for clipID := 1; clipID <= 3; clipID++ {
		d.processTranscript(clipID, fmt.Sprintf("transcript %d", clipID), true)
	}

	if _, err := os.Stat(filepath.Join(run, "transcripts", "1.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transcript below retention window still exists or stat failed differently: %v", err)
	}
	for clipID := 2; clipID <= 3; clipID++ {
		if _, err := os.Stat(filepath.Join(run, "transcripts", fmt.Sprintf("%d.txt", clipID))); err != nil {
			t.Fatalf("retained transcript %d is missing: %v", clipID, err)
		}
	}
}

func TestProcessTranscriptDoesNotPruneWhenRetentionDisabled(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{RuntimeDir: run}
	d := &daemon{
		cfg:    &cfg,
		log:    logger,
		notify: notifier.New(context.Background(), "", logger),
		ctx:    context.Background(),
	}

	for clipID := 1; clipID <= 2; clipID++ {
		d.processTranscript(clipID, fmt.Sprintf("transcript %d", clipID), true)
	}
	for clipID := 1; clipID <= 2; clipID++ {
		if _, err := os.Stat(filepath.Join(run, "transcripts", fmt.Sprintf("%d.txt", clipID))); err != nil {
			t.Fatalf("transcript %d was removed with retention disabled: %v", clipID, err)
		}
	}
}

func TestProcessTranscriptPrunesWhenOutputCommandExits(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimedir.WriteTranscript(run, 1, "old"); err != nil {
		t.Fatal(err)
	}
	outCmd := filepath.Join(run, "out")
	if err := os.WriteFile(outCmd, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir:                run,
		OutCmd:                    outCmd,
		TranscriptRetentionWindow: 1,
	}
	d := &daemon{
		cfg:                              &cfg,
		log:                              logger,
		notify:                           notifier.New(context.Background(), "", logger),
		ctx:                              context.Background(),
		transcriptRetentionHighWatermark: 1,
		protectedTranscripts:             make(map[int]struct{}),
	}

	d.processTranscript(2, "new", true)

	if _, err := os.Stat(filepath.Join(run, "transcripts", "1.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transcript below retention window still exists or stat failed differently: %v", err)
	}
	if _, err := os.Stat(filepath.Join(run, "transcripts", "2.txt")); err != nil {
		t.Fatalf("new transcript is missing: %v", err)
	}
}

func TestReleaseTranscriptRemovesProtectedFilesOutsideWindow(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir:                run,
		TranscriptRetentionWindow: 1,
	}
	d := &daemon{
		cfg:                  &cfg,
		log:                  logger,
		ctx:                  context.Background(),
		protectedTranscripts: make(map[int]struct{}),
	}

	for clipID := 1; clipID <= 3; clipID++ {
		d.protectTranscript(clipID)
		if _, err := runtimedir.WriteTranscript(run, clipID, fmt.Sprintf("transcript %d", clipID)); err != nil {
			t.Fatal(err)
		}
	}
	d.releaseTranscript(3)
	d.releaseTranscript(1)
	if _, err := os.Stat(filepath.Join(run, "transcripts", "1.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released transcript 1 still exists or stat failed differently: %v", err)
	}
	d.releaseTranscript(2)
	if _, err := os.Stat(filepath.Join(run, "transcripts", "2.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("released transcript 2 still exists or stat failed differently: %v", err)
	}
	if len(d.protectedTranscripts) != 0 {
		t.Fatalf("protected transcripts = %v, want empty", d.protectedTranscripts)
	}
}

func TestProtectedTranscriptDoesNotAdvanceRetentionWindow(t *testing.T) {
	run := t.TempDir()
	if err := runtimedir.PrepareDir(run, nil); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	logger := log.New(&stderr, "talk2text: ", 0)
	cfg := config.Config{
		RuntimeDir:                run,
		TranscriptRetentionWindow: 2,
	}
	d := &daemon{
		cfg:                  &cfg,
		log:                  logger,
		ctx:                  context.Background(),
		protectedTranscripts: make(map[int]struct{}),
	}

	for clipID, text := range []string{"one", "two"} {
		clipID++
		d.protectTranscript(clipID)
		if _, err := runtimedir.WriteTranscript(run, clipID, text); err != nil {
			t.Fatal(err)
		}
		d.releaseTranscript(clipID)
	}
	d.protectTranscript(3)
	if _, err := runtimedir.WriteTranscript(run, 3, "three"); err != nil {
		t.Fatal(err)
	}
	if d.transcriptRetentionHighWatermark != 2 {
		t.Fatalf("retention high-water mark = %d, want 2", d.transcriptRetentionHighWatermark)
	}
	for clipID := 1; clipID <= 3; clipID++ {
		if _, err := os.Stat(filepath.Join(run, "transcripts", fmt.Sprintf("%d.txt", clipID))); err != nil {
			t.Fatalf("transcript %d is missing while clip 3 is protected: %v", clipID, err)
		}
	}

	d.releaseTranscript(3)
	if _, err := os.Stat(filepath.Join(run, "transcripts", "1.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transcript 1 still exists after window advanced: %v", err)
	}
	for clipID := 2; clipID <= 3; clipID++ {
		if _, err := os.Stat(filepath.Join(run, "transcripts", fmt.Sprintf("%d.txt", clipID))); err != nil {
			t.Fatalf("retained transcript %d is missing: %v", clipID, err)
		}
	}
}

func TestProcessTranscriptRemovesProtectionAfterWriteFailure(t *testing.T) {
	run := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{
		RuntimeDir:                run,
		TranscriptRetentionWindow: 2,
	}
	d := &daemon{
		cfg:                  &cfg,
		log:                  logger,
		notify:               notifier.New(context.Background(), "", logger),
		ctx:                  context.Background(),
		protectedTranscripts: make(map[int]struct{}),
	}

	d.processTranscript(7, "text", true)
	if _, ok := d.protectedTranscripts[7]; ok {
		t.Fatal("failed transcript write remained protected")
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
