# Goal

`talk2text` is a minimal push-to-talk speech-to-text tool for Linux desktops.

While the record key is held, the microphone is recorded. When the key is released, recording stops. Usable clips are sent to the configured Whisper-compatible HTTP endpoint, and the returned text is written to a per-clip transcript file.

The intended runtime is a single `talk2text` executable with:

1. A long-running `daemon` mode.
2. Short-lived client subcommands for desktop shortcuts.

The daemon handles recording over IPC and keeps the microphone stream warm for a short idle window after each recording so repeated dictation has low key-press latency.

# Runtime Model

## Components

The runtime is split into:

1. `talk2text daemon`
   - Long-running daemon mode of the `talk2text` executable.
   - Owns audio capture, transcription, transcript file creation, output command execution, notifications, and runtime state.
   - Listens on a Unix socket under the runtime directory.
   - Started by the user's preferred session startup mechanism.

2. `talk2text` client subcommands
   - Small shortcut-facing command modes in the same executable.
   - Send commands to the daemon over the Unix socket and exit quickly.
   - Intended for desktop shortcut bindings.

3. Whisper-compatible HTTP endpoint
   - External service, not managed by this project.
   - Receives recorded clips and returns transcription responses.

## Portability Boundary

The daemon core should only depend on portable Linux concepts and project interfaces:

1. Runtime directory discovery.
2. Unix socket IPC.
3. Audio capture through the configured Go audio backend.
4. Recording session state.
5. Whisper HTTP requests.
6. Transcript file creation.
7. Output command execution.
8. External notification command execution.
9. Configuration and logs.

The daemon core should not directly depend on a specific window manager, terminal emulator, clipboard command, editor, notification command, or Linux distribution.

# Configuration and Runtime Files

There is no configuration file yet. Configuration is provided by command-line options and environment variables, with built-in defaults.

## Command-Line Options

The daemon command-line options should include:

1. Whisper endpoint URL
   - Passed as `--whisper-endpoint`.
   - Default: `http://127.0.0.1:8080/inference`.

2. Output command
   - Optional output command path.
   - Passed as `--out-cmd`.

3. Notification command
   - Notification command path.
   - Passed as `--notify-cmd`.

Daemon and client subcommands should both accept:

1. Runtime directory
   - Passed as `--runtime-dir`.
   - Overrides default runtime directory discovery.
   - Intended primarily for testing and isolated manual runs.
   - For client subcommands, this option only selects the Unix socket location.

## Environment Variables

Environment variables should configure lower-level tuning that does not need to be part of the normal daemon command line:

1. Recording behavior
   - `TALK2TEXT_MIN_DURATION_SECONDS`: minimum accepted duration in seconds, default `0.5`.
   - `TALK2TEXT_MAX_DURATION_SECONDS`: maximum recording duration in seconds, default `100`.
   - `TALK2TEXT_WARM_RETENTION_SECONDS`: warm retention window in seconds, default `15`.

2. Audio capture defaults
   - `TALK2TEXT_RECORD_INPUT_DEVICE`: input device, defaulting to the system default input device.

3. Whisper request behavior
   - `TALK2TEXT_WHISPER_CONNECT_TIMEOUT_SECONDS`: connect timeout in seconds, default `1`.
   - `TALK2TEXT_WHISPER_REQUEST_TIMEOUT_SECONDS`: request timeout in seconds after recording has stopped and the Whisper HTTP request has started, default `10`.

## Runtime Directory

Runtime files are stored under the configured runtime directory. When no runtime directory is configured explicitly, the default is the first usable directory in this order:

1. `$XDG_RUNTIME_DIR/talk2text` when `XDG_RUNTIME_DIR` is an existing directory.
2. Otherwise `$TMPDIR/run-<uid>/talk2text` when `TMPDIR` is an existing directory.
3. Otherwise `/tmp/run-<uid>/talk2text`.

The runtime directory may contain:

1. `daemon.sock`
2. `transcription-context`
3. `transcripts/<seq>.txt`

When the daemon creates the runtime directory or `transcripts` directory, it should create them with owner-only permissions, such as `0700`. When the daemon creates `transcription-context` or transcript files, it should create them with owner-only permissions, such as `0600`. If these paths already exist, the daemon should not chmod them.

The runtime directory and `transcripts` directory are usable only when they are directories owned by the current user. Existing paths that are not directories, are owned by a different user, or cannot be inspected should cause daemon startup to fail. The daemon checks ownership but does not reject existing directories based on their permission modes.

On daemon startup, the daemon should create `<runtime_dir>/transcripts` if needed and clean stale transcript files from that directory. Startup cleanup should remove only regular files directly under `<runtime_dir>/transcripts`; it should not recursively delete subdirectories or follow paths outside the transcript directory. If cleanup of an individual file fails, the daemon should log the failure and continue startup. If the transcript directory cannot be created or used, daemon startup should fail.

On daemon startup, if `daemon.sock` already exists, the daemon should attempt to connect to it:

1. If the connection succeeds, startup should fail because a daemon is already running.
2. If the connection fails, the daemon should treat the socket path as stale, remove it, and continue startup.
3. If `daemon.sock` exists but is not a socket, startup should fail rather than deleting the path.

After successfully binding the socket, the daemon should make the socket owner-only if supported. On shutdown, the daemon should remove `daemon.sock`.

# Logging

The daemon logs to stderr. It should not write a daemon log file during normal operation.

The daemon should log all errors. Informational logs should be limited to key daemon lifecycle events during startup and shutdown. The daemon should not log every client request, recording, transcription, output command, or notification event merely for informational tracing.

# CLI and Shortcut Model

The intended `talk2text` executable supports:

1. `daemon`
   - Runs the long-lived daemon process.
   - Intended to be launched by the user's preferred session startup mechanism.

2. `start`
   - Starts a recording session.
   - If already recording, discards the active session without transcription and starts a new session.

3. `stop`
   - Stops the active recording session.
   - If not recording, it should no-op.

4. `status`
   - Returns daemon status useful for scripts or status bars.
   - Should include recording state: `off`, `active`, or `warm`.
   - Should include the next daemon-local clip ID.
   - Should include the active clip ID when recording.
   - Should include the number of transcription requests that have been sent to the Whisper endpoint and are still waiting for responses.
   - Should include the daemon's runtime directory.
   - Should include the daemon's effective configuration from command-line options, environment variables, and defaults.

The daemon must be running before `start` and `stop` commands are used. Startup is user-managed; for example, Sway users may start it from their Sway config:

```sh
exec talk2text daemon --out-cmd /home/meng/bin/talk2text-output
```

The expected shortcut model uses a record key:

1. Record key press starts recording.
2. Record key release stops recording.

Example:

```sh
# Sway example
bindsym --no-repeat F12 exec talk2text start
bindsym --release F12 exec talk2text stop
```

# IPC Protocol

The daemon listens for connections on the Unix socket at `<runtime_dir>/daemon.sock`. The socket is an `AF_UNIX` stream socket, not a datagram socket.

The socket path is an address for connecting to the daemon. The daemon does not read `daemon.sock` as a regular file. Instead, it accepts client connections and reads bytes from each connected stream.

Because stream sockets do not preserve message boundaries, the IPC protocol uses newline-delimited UTF-8 messages. Each client connection sends one command line and receives one JSON response line.

Supported command lines:

1. `start`
2. `stop`
3. `status`

Unknown or malformed command lines should receive an unsuccessful JSON response.

Raw socket responses are JSON objects. Successful `start` and `stop` responses may be minimal, such as:

```json
{"ok":true}
```

The raw `status` response should include stable fields for scripts and status bars. It should report whether the command succeeded, the current recording state, the next daemon-local clip ID, the active clip ID when recording, the number of in-flight transcription requests, the runtime directory, and the daemon's effective configuration from command-line options, environment variables, and defaults.

The `talk2text status` client command should print human-readable output derived from the JSON response. The exact human-readable formatting is an implementation detail.

Client error message wording and prefixes are implementation details.

Client subcommands should use these exit codes:

1. `0`: success
2. `1`: daemon returned an unsuccessful response or command execution failed
3. `2`: invalid CLI usage
4. `3`: daemon unavailable because the socket is missing, unreachable, or does not accept a connection

When the daemon is unavailable, client subcommands should print a short error message to stderr and exit with code `3`.

# Audio Capture

The daemon records audio directly in-process without spawning `ffmpeg` for normal recording. The Go audio package choice is documented in [ADR 0001](decisions/0001-use-malgo-for-audio-capture.md).

## Capture Format

The daemon should first request this Whisper-friendly capture format from the audio backend:

1. signed 16-bit PCM
2. mono
3. 16 kHz sample rate
4. system default input device

If the audio backend cannot open the default input device with that format, the daemon may open the device using its native capture format. In that path, the daemon should convert in-process to signed 16-bit PCM mono, downsampling to 16 kHz only when the native sample rate is greater than 16 kHz. When the native sample rate is 16 kHz or lower, the daemon should preserve that sample rate.

The daemon should not globally force PipeWire, PulseAudio, ALSA, or hardware sample rates for the whole system.

## Warm Retention Window

The daemon should lazily open the microphone stream on first recording start.

After recording stops, the daemon should keep the stream open for the configured warm retention window. If another recording starts during this window, recording should begin without paying the full stream startup cost again.

When the warm retention timer expires while idle, the daemon closes the microphone stream.

This provides faster repeated recording without keeping the microphone open all day.

# Recording and Clip Lifecycle

The clip ID is a daemon-local sequence number that starts at `1` on daemon startup. Every accepted `start` consumes one clip ID, even if the clip is later short, discarded, failed, or auto-stopped. The transcript file path uses that sequence number, such as `<runtime_dir>/transcripts/<seq>.txt`. The daemon may truncate and overwrite stale transcript files from a previous daemon process.

## Concurrency

Recording is sequential: the daemon has at most one active recording session at a time.

Transcription and output processing may run concurrently for multiple stopped clips. The daemon does not serialize transcription or output command execution by clip ID. There is no explicit transcription concurrency limit for now. Completion order does not need to match clip sequence order.

## Start

On `start`, the daemon:

1. Discards the active session without transcription when a recording is already active.
2. Ensures an audio stream is open, opening it if needed.
3. Starts a new session with the next daemon-local sequence number as its clip ID.
4. Begins collecting audio frames for that session.
5. Schedules an informational `record-start` notification to be emitted only if the recording remains active for at least the configured minimum duration.

If the recording stops before the scheduled `record-start` notification is emitted, the clip is treated as `short`: `record-start` is not emitted and no transcription is attempted, but output processing still runs with output kind `short`.

## Stop

On `stop`, the daemon:

1. No-ops if there is no active recording session.
2. Stops collecting audio frames for the active session.
3. Starts or resets the warm retention timer for the microphone stream.
4. Classifies and processes the clip according to the rules below.

If the configured maximum recording duration is reached, the daemon should stop the recording automatically and process the clip.

## Clip Classification

Each stopped clip is classified into one output kind:

1. `short`
   - The clip was shorter than the configured minimum duration.
   - The clip is not sent for transcription.
   - The output text is empty.

2. `blank`
   - Transcription completed but returned empty text or `[BLANK_AUDIO]`.
   - The output text is empty.

3. `text`
   - Transcription returned non-empty text after cleaning.
   - The output text is the cleaned transcript text.

After a clip is classified, the daemon writes the output text to the per-clip transcript file, truncating any existing file at that path.

## Transcription

For clips at or above the configured minimum duration, the daemon:

1. Encodes the clip as WAV data in memory.
2. Emits an informational `record-stop` notification.
3. Emits an informational `transcribe-start` notification.
4. Sends a Whisper HTTP `POST` request to the configured endpoint using multipart form data.
5. Uploads the in-memory WAV data.
6. Includes a cleaned `prompt` form field from `transcription-context` when that file exists and is not empty.
7. Reads and parses the response body without writing it to disk.
8. Reads response text from `.text`.
9. Cleans repeated whitespace from the response text.

Recording time is not part of the Whisper request timeout. The request timeout starts after recording has stopped and the Whisper HTTP request has started.

If `transcription-context` does not exist, the daemon sends no prompt. If `transcription-context` exists but cannot be read, the daemon should log the failure, emit an error notification, and fail transcription for that clip without invoking the output command.

The request should include:

1. `temperature=0`
2. `temperature_inc=0.9`
3. `file=<in-memory WAV data>`
4. `response_format=json`
5. `prompt=<cleaned context>` when `transcription-context` exists and is non-empty

The daemon should not persist the raw response JSON during normal operation.

If transcription fails, including Whisper request timeout, connection failure, non-2xx response, invalid JSON, missing `.text`, or other Whisper endpoint errors, the daemon should log the failure and emit an error notification. Failed transcription should not create or write a per-clip transcript file and should not invoke the output command.

After a transcribed clip is written to the per-clip transcript file, the daemon emits an informational `transcribe-stop` notification that includes the file path in the display message.

## Output Processing

The daemon hands completed transcripts to an external output command. This keeps desktop, editor, and clipboard integrations outside the Go daemon without making the daemon responsible for destination routing.

The output command receives each per-clip transcript file with an output kind and performs any desktop- or editor-specific handling outside the daemon.

The daemon is configured with:

```sh
talk2text daemon --out-cmd /home/meng/bin/talk2text-output
```

When an output command is configured, the daemon invokes:

```sh
<output-command> <kind> <clip_transcript_file>
```

The output command may be any executable script or program. It can read the transcript text from the file path passed as its second argument.

Before invoking the configured output command, the daemon emits an informational `output-start` notification.

The daemon invokes the configured output command path as provided. It does not resolve symlinks, canonicalize the command path, or assume any particular routing mechanism behind the command.

The daemon does not impose a timeout on the output command. Output commands may be long-running or interactive, such as commands that coordinate with an editor. While an output command is running, cleanup for that clip waits for the command to exit.

The daemon owns transcript file cleanup:

1. After the configured output command exits successfully, the daemon removes the per-clip transcript file.
2. If no output command is configured, the transcript file remains as the final output.
3. If output processing fails, the daemon keeps the transcript file for inspection.

The daemon should treat a configured but missing, non-executable, or failing output command as an output failure: log details, emit an error notification, and keep the per-clip transcript file for inspection.

Example clipboard output command:

```sh
#!/usr/bin/env sh
kind="$1"
path="$2"
[ "$kind" = text ] || exit 0
wl-copy < "$path"
```

# Notification Command

The daemon emits notifications by invoking an external notification command. This keeps desktop notification behavior outside the Go daemon.

The daemon may be configured with:

```sh
talk2text daemon --notify-cmd /home/meng/bin/talk2text-notify
```

If `--notify-cmd` is not set, the daemon should suppress notifications.

The daemon invokes the configured notification command path as provided for each notification. It does not resolve symlinks, canonicalize the command path, or assume any particular routing mechanism behind the command.

The daemon starts the notification command asynchronously as:

```sh
<notification-command> <level> <code> <message>
```

Arguments:

1. `<level>` is either `info` or `error`.
2. `<code>` is a stable event or error-source code.
3. `<message>` is fallback display text.

For `info` notifications, `<code>` identifies the lifecycle event. Event codes are:

1. `record-start`
2. `record-stop`
3. `transcribe-start`
4. `transcribe-stop`
5. `output-start`

For `error` notifications, `<code>` identifies the error source, such as `config`, `runtime`, `audio-capture`, `whisper`, or `output-command`. IPC request errors are returned to the client and should not emit user notifications.

Error notification messages should briefly identify:

1. The stage where the error happened: recording, transcribing, or output processing.
2. An identifier when available, such as the daemon-local clip sequence number.

Detailed diagnostics, including stderr, response bodies, stack details, and low-level error values, belong in stderr logs, not in the notification message. The exact notification text is an implementation detail.

Example notification command:

```sh
#!/usr/bin/env sh
level="$1"
code="$2"
message="$3"
notify-send -t 5000 'talk2text' "$message"
```

The daemon does not impose a timeout on notification commands. Notification command startup failures should be logged but should not fail recording, transcription, or output handling. Notification command failures must not trigger another notification attempt. The daemon should reap notification command processes in the background. Later exit failures may also be logged.

# Whisper Endpoint

The daemon expects a Whisper-compatible HTTP endpoint at `http://127.0.0.1:8080/inference` by default. The endpoint is external to this project and must already be running.

# Installation and Dependencies

This project has trivial installation requirements: build the `talk2text` executable and place it somewhere on `PATH`.

The daemon/client runtime expects:

1. `talk2text`
2. an external Whisper-compatible HTTP endpoint
3. a Linux audio stack supported by the selected Go audio backend

Building `talk2text` requires Go.

# Shutdown

On SIGINT or SIGTERM, the daemon should shut down promptly:

1. Stop accepting new IPC connections.
2. Close the Unix socket listener.
3. Remove `daemon.sock`.
4. Discard any active recording without transcription.
5. Cancel or abandon in-flight transcription work.
6. Stop waiting for any already-started output or notification commands without intentionally killing them.
7. Close the microphone stream and any other owned resources.
8. Exit without waiting for graceful completion of transcription or output work.

Shutdown should prioritize avoiding leaked resources over preserving unfinished clips or leftover transcripts.

# Future Considerations

1. Transcription concurrency limit
   - The initial implementation has no explicit concurrency limit because recording is sequential.
   - If repeated valid recordings can overwhelm the Whisper endpoint or retain too much in-memory audio, add a configurable maximum number of in-flight transcription requests.
