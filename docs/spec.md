# talk2text

## Goal
A minimal push-to-talk speech-to-text tool for Linux desktops.

While the record key is held, audio is recorded from the microphone. When the key is released, recording stops. The usable clip is sent to a local Whisper HTTP server, and the returned text is routed to the currently selected output target.

The runtime should optimize for low key-press latency. The long-term implementation is a Go daemon that stays running, handles recording over IPC, and keeps the microphone stream warm for a short idle window after each recording.

The daemon core should be agnostic about the window manager and specific Linux distribution. Default packaged adapters and installer behavior may be opinionated for the current primary environment: Wayland, Sway, and Debian-like systems.

## Runtime Model

### Components
The runtime is split into:

1. `talk2textd`
   - Long-running Go daemon.
   - Owns audio capture, transcription, target routing, notifications, and runtime state.
   - Listens on a Unix socket under the runtime directory.
   - Starts automatically as a user `systemd` service.

2. `talk2textctl`
   - Small shortcut-facing client.
   - Sends commands to `talk2textd` over the Unix socket and exits quickly.
   - Intended for desktop shortcut bindings.

3. `whisper-server`
   - Local `whisper.cpp` HTTP server.
   - Keeps the Whisper model loaded between requests.

### Portability Boundary
The daemon core should only depend on portable Linux concepts and project interfaces:

1. Runtime directory discovery.
2. Unix socket IPC.
3. Audio capture through the configured Go audio backend.
4. Recording session state.
5. Whisper HTTP requests.
6. Output target interfaces.
7. Notification interfaces.
8. Configuration and logs.

The daemon core should not directly depend on a specific window manager, terminal emulator, clipboard command, editor, notification command, or Linux distribution.

Environment-specific behavior belongs in adapters or packaging:

1. Wayland clipboard support belongs in a clipboard target adapter.
2. Sway window focus/open behavior belongs in a popup editor target adapter.
3. Neovim integration belongs in an editor target adapter or companion plugin.
4. Desktop notifications belong in notifier adapters.
5. Debian-specific installation choices belong in the installer or packaging layer.

### Runtime Directory
Runtime files are stored under:

1. `$XDG_RUNTIME_DIR/talk2text` when `XDG_RUNTIME_DIR` is an existing directory.
2. Otherwise `$TMPDIR/run-<uid>/talk2text` when `TMPDIR` is an existing directory.
3. Otherwise `/tmp/run-<uid>/talk2text`.

The runtime directory may contain:

1. `talk2textd.sock`
2. `transcript`
3. `transcription-context`
4. `<clip_id>.wav`
5. `<clip_id>.response.json`
6. `talk2textd.log`

The selected output target is runtime-only in daemon memory and is not persisted.

### Configuration
Stable preferences are configured outside the runtime directory. The exact config path is implementation-defined, but should follow XDG conventions.

The configuration should include:

1. Audio capture format:
   - input backend/device
   - channels, default `1`
   - sample rate, default `16000`
   - sample format, default signed 16-bit PCM

2. Recording behavior:
   - minimum accepted duration, default `0.5s`
   - maximum recording duration, default `100s`
   - warm retention window, default to a short value such as `15s`

3. Whisper server:
   - server URL, default `http://127.0.0.1:9898/inference`
   - connect timeout
   - request timeout

4. Output targets:
   - `default_target`, persistent
   - `target_order`, persistent
   - per-target settings

On daemon startup, `current_target` is initialized from `default_target`. Switching targets only changes in-memory state until the daemon exits.

## Audio Capture

### Go Audio Backend
The preferred Go package is `github.com/gen2brain/malgo`, the Go binding for `miniaudio`.

The initial implementation should request the direct Whisper-friendly format from the capture API:

1. signed 16-bit PCM
2. mono
3. 16 kHz sample rate

The daemon should not spawn `ffmpeg` for normal recording.

### Warm Retention Window
The daemon should lazily open the microphone stream on first recording start.

After recording stops, the daemon should keep the stream open for the configured warm retention window. If another recording starts during this window, recording should begin without paying the full stream startup cost again.

When the warm retention timer expires while idle, the daemon closes the microphone stream.

This provides faster repeated dictation without keeping the microphone open all day.

### Native Format Fallback
The first implementation should request mono 16 kHz directly. If that later proves slow, unreliable, or low quality on a target system, a future implementation may capture at the device or audio-server native format, then downmix and resample in-process before transcription.

The daemon should not globally force PipeWire, PulseAudio, ALSA, or hardware sample rates for the whole system.

## CLI Commands

`talk2textctl` supports:

1. `start`
   - Starts a recording session.
   - Captures the selected output target at session start.
   - If already recording, it should either no-op or return a clear error.

2. `stop`
   - Stops the active recording session.
   - If not recording, it should no-op.

3. `target-next`
   - Advances to the next configured available target in `target_order`.
   - Emits a notification with the selected target.
   - Does not persist the selected target.

4. `status`
   - Returns daemon status useful for scripts or status bars.
   - Should include whether recording is active, current target, default target, and available targets.

Optional future commands:

1. `target <name>`
   - Selects a target by name for the current daemon lifetime.

2. `targets`
   - Lists configured targets and availability.

## Shortcut Binding Model

The expected key model uses at most two shortcut keys:

1. Record key:
   - press starts recording
   - release stops recording

2. Target switch key:
   - cycles the selected output target

Example:

```sh
# Sway example
bindsym --no-repeat F12 exec talk2textctl start
bindsym --release F12 exec talk2textctl stop
bindsym F11 exec talk2textctl target-next
```

## Recording Workflow

On `start`, `talk2textd`:

1. Ensures an audio stream is open, opening it if needed.
2. Starts a new session with a unique clip ID.
3. Captures the current output target into the session.
4. Begins collecting audio frames for that session.
5. Emits a recording notification.

On `stop`, `talk2textd`:

1. Stops collecting audio frames for the active session.
2. Ignores clips shorter than the configured minimum duration.
3. Writes a WAV file for the clip.
4. Starts or resets the warm retention timer for the microphone stream.
5. Sends the WAV file to the Whisper server.
6. Includes a cleaned `prompt` form field from `transcription-context` when it is not empty.
7. Drops empty transcripts or those containing only `[BLANK_AUDIO]`.
8. Routes the transcript to the session's captured output target.
9. Removes temporary per-clip files after successful processing.

If the maximum recording duration is reached, the daemon should stop the recording automatically and process the clip.

## Transcription

The daemon sends clips to the Whisper server using HTTP multipart form data.

The request should include:

1. `temperature=0`
2. `temperature_inc=0.9`
3. `file=@<clip_id>.wav`
4. `response_format=json`
5. `prompt=<cleaned context>` when `transcription-context` is non-empty

The response text is read from `.text`, cleaned of repeated whitespace, and ignored when empty or equal to `[BLANK_AUDIO]`.

## Output Targets

Output targets are code-level adapters inside the daemon project. The daemon may contain concrete target implementations, but the recording and transcription core should only depend on a target interface.

The selected target for a recording is captured at `start`. Switching targets during a recording affects the next recording, not the active one.

Target switching should skip unavailable targets when possible.

### Target Interface
Conceptually:

```go
type OutputTarget interface {
    Name() string
    Available(ctx context.Context) bool
    Handle(ctx context.Context, event TranscriptEvent) error
}
```

### Transcript Event
Conceptually:

```json
{
  "type": "transcript",
  "id": "20260630T123456.123",
  "text": "hello world",
  "target": "clipboard-wayland",
  "created_at": "2026-06-30T12:34:56-04:00",
  "transcript_file": "/run/user/1000/talk2text/transcript",
  "context_file": "/run/user/1000/talk2text/transcription-context"
}
```

### Built-In Targets

1. `clipboard-wayland`
   - Copies the transcript text to the Wayland clipboard using `wl-copy`.
   - Availability requires `wl-copy`.

2. `popup-nvim-sway`
   - Appends transcript text to the runtime transcript file.
   - Opens or focuses a Sway/Alacritty/Neovim editor similar to the current script behavior.
   - Availability requires the configured terminal, `swaymsg`, `nvim`, and related helper commands.

3. `nvim-buffer`
   - Sends the transcript to an already-running Neovim integration.
   - Availability requires a plugin-registered runtime endpoint.
   - The target must be considered ephemeral and should not be used as persisted daemon state.

## Notifications

Notifications are code-level adapters inside the daemon project. The recording and transcription core should depend on a notification interface rather than shelling out directly.

Conceptually:

```go
type Notifier interface {
    RecordingStarted(ctx context.Context, session SessionInfo) error
    Transcribing(ctx context.Context, session SessionInfo) error
    TranscriptReady(ctx context.Context, event TranscriptEvent) error
    TargetChanged(ctx context.Context, target string) error
    Error(ctx context.Context, err error) error
}
```

Initial implementations:

1. `freedesktop`
   - Uses desktop notifications, for example through `notify-send` or a native D-Bus implementation.

2. `noop`
   - Suppresses notifications.

## Adapter Registry

Output targets and notification backends should be registered through small registries so adding a new adapter is local and does not require changing recorder/transcriber code.

Conceptually:

```go
type TargetFactory func(Config) (OutputTarget, error)

type TargetRegistry struct {
    targets map[string]TargetFactory
}
```

Adding a target should look like registering a name and constructor:

```go
registry.Register("clipboard-wayland", clipboardwayland.New)
registry.Register("popup-nvim-sway", popupnvimsway.New)
registry.Register("nvim-buffer", nvimbuffer.New)
```

## Whisper Server
The runtime expects an HTTP endpoint at `http://127.0.0.1:9898/inference` by default. The installer provisions `whisper.cpp` `whisper-server` as a user `systemd` service and keeps the model loaded between requests.

## Installer
`install` supports:

1. `--install` to build or update the managed `whisper-server`, install user services, build/copy `talk2textd` and `talk2textctl` into `~/bin`, and install default configuration.
2. `--uninstall` to remove installed runtime binaries and, when the managed `~/bin/whisper-server` exists, remove that binary, the user service, and local `whisper.cpp` build artifacts.

The installer leaves the repository checkout and downloaded model files in place.

## Runtime Dependencies

The daemon/client runtime expects:

1. `talk2textd`
2. `talk2textctl`
3. local Whisper HTTP server
4. Linux audio stack supported by `malgo`/`miniaudio`

Some adapters add optional runtime dependencies:

1. `clipboard-wayland`: `wl-copy`
2. `popup-nvim-sway`: `swaymsg`, `alacritty` or configured terminal, `nvim`, `jq` if needed by the adapter implementation
3. `freedesktop` notifications: `notify-send` or D-Bus notification support

The installer additionally expects `systemctl`, `cmake`, `make`, `git`, `curl`, and Go.

## Non-Goals

1. Typing injection
2. Streaming partial transcripts into targets while recording
3. Multi-language orchestration
4. Noise suppression
5. Waybar integration
6. Persisting the runtime-selected output target across daemon restarts
