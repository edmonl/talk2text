# talk2text

## Goal
A minimal push-to-talk speech-to-text tool for Linux desktops.

While the record key is held, audio is recorded from the microphone. When the key is released, recording stops. The usable clip is sent to the configured Whisper-compatible HTTP endpoint, and the returned text is passed to the configured output command.

The runtime should optimize for low key-press latency. The long-term implementation is a Go daemon that stays running, handles recording over IPC, and keeps the microphone stream warm for a short idle window after each recording.

## Runtime Model

### Components
The runtime is split into:

1. `talk2text daemon`
   - Long-running daemon mode of the `talk2text` executable.
   - Owns audio capture, transcription, output command routing, notifications, and runtime state.
   - Listens on a Unix socket under the runtime directory.
   - Started by the user's preferred session startup mechanism.

2. `talk2text` client subcommands
   - Small shortcut-facing command modes in the same executable.
   - Send commands to the daemon over the Unix socket and exit quickly.
   - Intended for desktop shortcut bindings.

3. Whisper-compatible HTTP endpoint
   - External service, not managed by this project.
   - Receives recorded clips and returns transcription responses.

### Portability Boundary
The daemon core should only depend on portable Linux concepts and project interfaces:

1. Runtime directory discovery.
2. Unix socket IPC.
3. Audio capture through the configured Go audio backend.
4. Recording session state.
5. Whisper HTTP requests.
6. External output command execution.
7. External notification command execution.
8. Configuration and logs.

The daemon core should not directly depend on a specific window manager, terminal emulator, clipboard command, editor, notification command, or Linux distribution.

Environment-specific behavior belongs in output commands, notification commands, or packaging:

1. Wayland clipboard support belongs in an output command.
2. Sway window focus/open behavior belongs in an output command.
3. Neovim integration belongs in an output command or companion plugin.
4. Desktop notifications belong in a notification command.
5. Distribution-specific installation choices belong outside the core daemon.

### Runtime Directory
Runtime files are stored under:

1. `$XDG_RUNTIME_DIR/talk2text` when `XDG_RUNTIME_DIR` is an existing directory.
2. Otherwise `$TMPDIR/run-<uid>/talk2text` when `TMPDIR` is an existing directory.
3. Otherwise `/tmp/run-<uid>/talk2text`.

The runtime directory may contain:

1. `talk2text.sock`
2. `transcript`
3. `transcription-context`
4. `<clip_id>.wav`
5. `<clip_id>.response.json`
6. `<clip_id>.txt`
7. `talk2text.log`

The selected output command is captured per recording session and is not persisted.

### Configuration
For now, there is no configuration file. Configuration is provided by command-line options to `talk2text`, with built-in defaults.

Configurable options should include:

1. Audio capture format:
   - input backend/device
   - channels, default `1`
   - sample rate, default `16000`
   - sample format, default signed 16-bit PCM

2. Recording behavior:
   - minimum accepted duration, default `0.5s`
   - maximum recording duration, default `100s`
   - warm retention window, default to a short value such as `15s`

3. Whisper endpoint:
   - endpoint URL, default `http://127.0.0.1:9898/inference`
   - connect timeout
   - request timeout

4. Output command:
   - output command path, passed as `--output-cmd`

5. Notification command:
   - notification command path, passed as `--notification-cmd`

The output command path may be a symlink. This allows external tools, such as a Neovim plugin, to change the effective destination while the daemon keeps running.

## Audio Capture

### Capture Format
The daemon records audio directly in-process without spawning `ffmpeg` for normal recording. The Go audio package choice is documented in [ADR 0001](decisions/0001-use-malgo-for-audio-capture.md).

The initial implementation should request the direct Whisper-friendly format from the capture API:

1. signed 16-bit PCM
2. mono
3. 16 kHz sample rate

### Warm Retention Window
The daemon should lazily open the microphone stream on first recording start.

After recording stops, the daemon should keep the stream open for the configured warm retention window. If another recording starts during this window, recording should begin without paying the full stream startup cost again.

When the warm retention timer expires while idle, the daemon closes the microphone stream.

This provides faster repeated dictation without keeping the microphone open all day.

### Native Format Fallback
The first implementation should request mono 16 kHz directly. If that later proves slow, unreliable, or low quality on a target system, a future implementation may capture at the device or audio-server native format, then downmix and resample in-process before transcription.

The daemon should not globally force PipeWire, PulseAudio, ALSA, or hardware sample rates for the whole system.

## CLI Commands

`talk2text` supports:

1. `daemon`
   - Runs the long-lived daemon process.
   - Intended to be launched by the user's preferred session startup mechanism.

2. `start`
   - Starts a recording session.
   - Captures the resolved output command at session start.
   - If already recording, discards the active session without transcription and starts a new session.

3. `stop`
   - Stops the active recording session.
   - If not recording, it should no-op.

4. `status`
   - Returns daemon status useful for scripts or status bars.
   - Should include whether recording is active, the configured output command path, and the configured notification command path.

## Shortcut Binding Model

The daemon must be running before `start` and `stop` commands are used. Startup is user-managed; for example, Sway users may start it from their Sway config:

```sh
exec talk2text daemon --output-cmd /run/user/1000/talk2text/current-output
```

The expected shortcut model uses a record key:

1. Record key:
   - press starts recording
   - release stops recording

Example:

```sh
# Sway example
bindsym --no-repeat F12 exec talk2text start
bindsym --release F12 exec talk2text stop
```

## Recording Workflow

On `start`, the daemon:

1. Discards the active session without transcription if already recording.
2. Ensures an audio stream is open, opening it if needed.
3. Starts a new session with a unique clip ID.
4. Resolves the output command path and captures the resolved command path into the session.
5. Begins collecting audio frames for that session.
6. Emits a `recording-started` notification event.

On `stop`, the daemon:

1. No-ops if there is no active recording session.
2. Stops collecting audio frames for the active session.
3. Ignores clips shorter than the configured minimum duration.
4. Writes a WAV file for the clip.
5. Starts or resets the warm retention timer for the microphone stream.
6. Emits a `transcribing` notification event.
7. Sends the WAV file to the configured Whisper endpoint.
8. Includes a cleaned `prompt` form field from `transcription-context` when it is not empty.
9. Drops empty transcripts or those containing only `[BLANK_AUDIO]`.
10. Writes the cleaned transcript to a per-clip transcript file.
11. Emits a `transcript-ready` notification event.
12. Invokes the session's captured output command with the per-clip transcript file path as its first argument.
13. Removes temporary per-clip files after successful processing.

If the maximum recording duration is reached, the daemon should stop the recording automatically and process the clip.

## Transcription

The daemon sends clips to the configured Whisper-compatible HTTP endpoint using multipart form data.

The request should include:

1. `temperature=0`
2. `temperature_inc=0.9`
3. `file=@<clip_id>.wav`
4. `response_format=json`
5. `prompt=<cleaned context>` when `transcription-context` is non-empty

The response text is read from `.text`, cleaned of repeated whitespace, and ignored when empty or equal to `[BLANK_AUDIO]`.

## Output Command

The daemon routes transcripts by invoking an external output command. This keeps desktop, editor, and clipboard integrations outside the Go daemon.

The daemon is configured with:

```sh
talk2text daemon --output-cmd /run/user/1000/talk2text/current-output
```

The output command path may be a symlink. External tools may atomically update the symlink to switch the destination while the daemon keeps running.

The daemon resolves and captures the output command path at `start`. Switching the symlink during a recording affects the next recording, not the active one.

After transcription succeeds, the daemon writes the cleaned transcript text to a per-clip transcript file and invokes:

```sh
<captured-output-command> <clip_transcript_file>
```

The output command may be any executable script or program. It can read the transcript text from the file path passed as its first argument.

Example clipboard output command:

```sh
#!/usr/bin/env sh
wl-copy < "$1"
```

Example symlink switch:

```sh
ln -sfn "$new_output_cmd" "$runtime_dir/current-output.tmp"
mv -Tf "$runtime_dir/current-output.tmp" "$runtime_dir/current-output"
```

The daemon should treat a missing, non-executable, or failing output command as an output failure: log stderr, emit an `error` notification event, and keep the per-clip transcript file for inspection.

## Notification Command

The daemon emits notifications by invoking an external notification command. This keeps desktop notification behavior outside the Go daemon.

The daemon may be configured with:

```sh
talk2text daemon --notification-cmd /home/meng/bin/talk2text-notify
```

If `--notification-cmd` is not set, the daemon should suppress notifications.

The notification command path may be a symlink. The daemon invokes the configured path for each event, so external tools may update the symlink to change notification behavior while the daemon keeps running.

The daemon invokes the notification command as:

```sh
<notification-command> <event> [detail]
```

Events:

1. `recording-started`
2. `transcribing`
3. `transcript-ready`
   - detail: per-clip transcript file path
4. `error`
   - detail: error summary

Example notification command:

```sh
#!/usr/bin/env sh
event="$1"
detail="${2:-}"
notify-send -t 5000 'talk2text' "$event $detail"
```

Notification command failures should be logged but should not fail recording, transcription, or output handling.

## Whisper Endpoint
The daemon expects a Whisper-compatible HTTP endpoint at `http://127.0.0.1:9898/inference` by default. The endpoint is external to this project and must already be running.

## Installation
This project has trivial installation requirements: build the `talk2text` executable and place it somewhere on `PATH`.

Whisper endpoint setup is external to this project.

## Runtime Dependencies

The daemon/client runtime expects:

1. `talk2text`
2. external Whisper-compatible HTTP endpoint
3. Linux audio stack supported by the selected Go audio backend

Some output and notification commands add optional runtime dependencies:

1. clipboard output command: `wl-copy`
2. popup Neovim output command: `swaymsg`, `alacritty` or configured terminal, `nvim`, `jq` if needed by the script
3. notification command: `notify-send` or D-Bus notification support

Building `talk2text` requires Go.

## Future Considerations

1. `talk2text output <path>`
   - Updates the output command symlink or another configured output pointer.

2. Output command metadata
   - A future descriptor format could add display names, command arguments, availability checks, and richer status.

3. Notification command metadata
   - A future descriptor format could add display names, supported events, and richer status.

## Open Questions

1. Should the daemon require `--output-cmd` to resolve to an executable at startup, or only when a recording starts?
2. Should failed output commands leave per-clip WAV files in addition to transcript files?
3. Should the daemon provide a helper command for atomically updating the output command symlink?
4. Should the daemon require `--notification-cmd` to resolve to an executable at startup, or only when an event is emitted?

## Non-Goals

1. Typing injection
2. Streaming partial transcripts into output commands while recording
3. Multi-language orchestration
4. Noise suppression
5. Waybar integration
6. Persisting editor focus or other dynamic output destination state inside the daemon
