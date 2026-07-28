# Goal

`talk2text` is a minimal Linux speech-to-text tool with local push-to-talk capture and optional HTTP audio submission.

A single executable provides a long-running daemon and short-lived client commands. The daemon owns audio capture, transcription, transcript files, output commands, notifications, and runtime state. Desktop shortcuts call the clients over a Unix socket. A separately managed Whisper-compatible HTTP service performs transcription.

The daemon stays independent of any particular window manager, editor, clipboard, notification system, or Linux distribution. Those integrations belong in external commands and user configuration.

# Configuration

There is no configuration file. Command-line options, environment variables, and built-in defaults configure the daemon.

Durations use Go-style values with explicit units, such as `500ms` or `15s`. They must be `0` or at least `10ms`.

Daemon options:

1. `--whisper-endpoint`, default `http://127.0.0.1:8080/inference`
2. `--out-cmd`, optional transcript output command
3. `--notify-cmd`, optional notification command
4. `--http-listen`, optional AMR-WB HTTP listener
5. `--allow-client-env`, repeatable additional request-environment allowlist
6. `--runtime-dir`, runtime directory override

Client commands also accept `--runtime-dir`. `start` accepts repeatable `--send-env` options as specified in [Request Environment and Output Routing](spec/request-environment.md).

Environment variables:

1. `TALK2TEXT_MIN_DURATION`, default `500ms`
2. `TALK2TEXT_MAX_DURATION`, default `100s`; `0s` disables local auto-stop and is invalid with HTTP input
3. `TALK2TEXT_STOP_DELAY`, default `250ms`
4. `TALK2TEXT_WARM_RETENTION`, default `15s`
5. `TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW`, default `100`; `0` disables runtime retention
6. `TALK2TEXT_RECORD_INPUT_DEVICE`, default system input device
7. `TALK2TEXT_WHISPER_CONNECT_TIMEOUT`, default `1s`
8. `TALK2TEXT_WHISPER_REQUEST_TIMEOUT`, default `10s`

# Runtime Directory

`--runtime-dir` overrides discovery. Otherwise the daemon uses the first applicable location:

1. `$XDG_RUNTIME_DIR/talk2text`
2. `$TMPDIR/run-<uid>/talk2text`
3. `/tmp/run-<uid>/talk2text`

Configured base paths must be absolute existing directories. Runtime directories must be owned by the current user. The daemon creates its own directories and files with owner-only permissions.

The runtime directory contains:

1. `daemon.sock`, the owner-only IPC socket
2. `transcription-prompt`, optional Whisper prompt text
3. `transcripts/<clip-id>`, per-clip output files

Startup creates the transcript directory and removes stale regular transcript files without following links or recursively deleting directories. Runtime transcript cleanup is specified in [Transcript File Retention](spec/transcript-retention.md).

Startup fails if a live daemon already owns `daemon.sock` or if the path is not a socket. A stale socket is removed. Shutdown removes the daemon socket.

# Commands

The executable supports:

1. `talk2text daemon`: run the daemon
2. `talk2text start`: start or replace a local recording
3. `talk2text stop`: stop the active local recording; no-op when idle
4. `talk2text status`: report recording state, clip IDs, in-flight transcriptions, runtime directory, and effective configuration

The intended shortcut model starts recording on key press and stops on release:

```sh
bindsym --no-repeat F12 exec talk2text start
bindsym --release F12 exec talk2text stop
```

Client exit codes:

1. `0`: success
2. `1`: unsuccessful daemon response or command failure
3. `2`: invalid CLI usage
4. `3`: daemon unavailable

# IPC Protocol

The daemon accepts JSON over `<runtime_dir>/daemon.sock`. The first complete JSON value on a connection is the request and must be an object. Later bytes are ignored. Decoding is bounded, and malformed or oversized first values are rejected under the same validation error.

Commands are `start`, `stop`, and `status`. `start` and `stop` may include an `env` object:

```json
{"command":"start","env":{"XDG_SESSION_ID":"3","WAYLAND_DISPLAY":"wayland-1"}}
```

Request environments and recording ownership are specified in [Request Environment and Output Routing](spec/request-environment.md).

Each accepted request receives one JSON response. Successful `start` and `stop` responses acknowledge acceptance without waiting for the requested work to finish, as recorded in [ADR 0006](decisions/0006-use-acknowledgement-only-start-and-stop-responses.md). Status responses expose stable data for scripts and status bars.

# Audio Input

## Local Capture

The daemon captures signed 16-bit mono PCM at 16 kHz through the configured Go audio backend. It uses the configured input device or the system default selected when the stream opens.

The microphone stream opens lazily. After recording, it remains open for the warm-retention window to reduce repeated start latency, then closes while idle. The daemon never changes the input device during an open stream.

## HTTP Submission

An optional `POST /transcribe` endpoint accepts AMR-WB clips without controlling the local microphone. Its request, response, security, and concurrency contracts are specified in [HTTP Audio Submission](spec/http-transcription.md).

# Recording and Clip Lifecycle

Clip IDs are daemon-local sequence numbers starting at `1`. Every accepted local start and HTTP submission consumes one ID. Rejected requests do not. Local recording is sequential, while transcription and output for completed clips may overlap and finish out of order.

## Start and Stop

An accepted `start` begins a new recording. If the same owner already has an active recording, the existing session is replaced; a session already pending delayed stop is finalized instead of discarded. Starts from other sessions follow the ownership rules in [Request Environment and Output Routing](spec/request-environment.md).

An accepted owner `stop` acknowledges immediately, continues capture for `TALK2TEXT_STOP_DELAY`, then finalizes the clip. Repeated stops do not extend the delay. A new accepted start finalizes a clip waiting on delayed stop. Reaching the configured maximum duration also finalizes the recording.

After stop, the daemon classifies and processes the clip.

## Classification

Every processed local or HTTP clip has one output kind:

1. `short`: PCM duration is below the minimum; no Whisper request; empty output
2. `blank`: Whisper returns empty text or `[BLANK_AUDIO]`; empty output
3. `text`: Whisper returns non-empty cleaned text

# Transcription

Usable PCM is encoded as WAV in memory and posted to the configured Whisper-compatible endpoint. The multipart request includes the audio, JSON response format, deterministic temperature settings, and cleaned `transcription-prompt` content when available.

The daemon reads `.text` from the JSON response and collapses repeated whitespace. Audio and response bodies remain in memory during normal processing.

Transcription failures are logged and notified. They do not create a transcript file or invoke the output command.

# Output Processing

When an output command is configured, completed output text is written to `transcripts/<clip-id>` and the command receives that path as its only argument:

```sh
TALK2TEXT_OUTPUT_KIND=<short|blank|text> \
TALK2TEXT_NOTIFY_CMD=<notification-command> \
<output-command> <transcript-path>
```

`TALK2TEXT_NOTIFY_CMD` is empty when notifications are disabled. Daemon-owned metadata overrides inherited or request-provided values. The command receives stderr from the daemon and may run asynchronously for any duration.

Without an output command, nonempty transcript text remains in its per-clip file. An output command owns transcript cleanup after successful processing, including delegated work. The daemon does not infer success from its exit status. Remaining files are governed by [Transcript File Retention](spec/transcript-retention.md). This ownership decision is recorded in [ADR 0007](decisions/0007-make-output-commands-responsible-for-transcript-cleanup.md).

Failure to start the output command is logged and notified. Once started, the command owns detailed failure reporting.

# Notifications and Logging

Notification behavior is specified in [Notification Command](spec/notification.md). Logging behavior is specified in [Logging](spec/log.md).

# Installation

Build the `talk2text` executable and place it on `PATH`. Runtime requires an external Whisper-compatible endpoint. Local capture also requires a supported Linux audio stack.

Building requires Go 1.26 and a C toolchain for cgo. The vendored AMR-WB decoder requires no separate development package.

# Shutdown

On SIGINT or SIGTERM, the daemon stops accepting work, removes its socket, discards active or unfinished capture and transcription work, closes owned resources, and exits promptly. Already-started output commands are not intentionally killed. Shutdown prioritizes avoiding leaked resources over preserving unfinished clips.
