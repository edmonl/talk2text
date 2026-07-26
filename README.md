## Overview

`talk2text` is a minimalist speech-to-text tool for Linux desktops. Hold a configured key to record locally, or submit AMR-WB audio through the optional HTTP input. Usable audio is sent to a Whisper-compatible HTTP server, and the transcript can then be copied to the clipboard or handled by any executable you choose.

The project consists of one executable running as either a daemon or short-lived client commands designed for desktop key bindings:

```text
key press    -> talk2text start -> record microphone
key release  -> talk2text stop  -> transcribe -> run output command
mobile audio -> POST /transcribe -> transcribe -> run output command
```

The daemon keeps the microphone stream open briefly after each recording so repeated recording starts quickly. Desktop-specific behavior can be configured as external commands for handling transcripts and notifications.

## Quick Start

Start a Whisper-compatible server. The default endpoint is the `whisper.cpp` server at `http://127.0.0.1:8080/inference`; use `--whisper-endpoint` if yours is different.

From a local checkout, install the included clipboard and notification commands:

```sh
install -Dm755 docs/examples/talk2text-copy-clipboard \
  "$HOME/.local/bin/talk2text-copy-clipboard"
install -Dm755 docs/examples/talk2text-notify-send \
  "$HOME/.local/bin/talk2text-notify-send"
```

The clipboard command uses the first available program among `wl-copy`, `xclip`, and `xsel`. The notification command requires `notify-send`.

Start the daemon in a terminal for an initial test:

```sh
talk2text daemon \
  --out-cmd "$HOME/.local/bin/talk2text-copy-clipboard" \
  --notify-cmd "$HOME/.local/bin/talk2text-notify-send"
```

In another terminal, record a clip:

```sh
talk2text start
# Speak for at least half a second.
talk2text stop
```

When transcription succeeds, the example output command copies the text to the clipboard and removes its transcript file.

Check the daemon and its effective configuration with:

```sh
talk2text status
```

## Installation

### System Dependencies

Building requires Go 1.26 and a C build toolchain for `cgo`. The audio library is self-contained and does not require audio development headers.

On Debian or Ubuntu:

```sh
sudo apt install build-essential
```

On Fedora:

```sh
sudo dnf install gcc
```

Local microphone capture requires a working Linux audio system supported by miniaudio, such as PipeWire, PulseAudio, ALSA, or JACK.

### Install With Go

Install the latest version from the module path:

```sh
go install github.com/edmonl/talk2text/cmd/talk2text@latest
```

Make sure Go's install directory is on the `PATH` used by your desktop session.

### Build From Local Checkout

From the project directory:

```sh
go install ./cmd/talk2text
```

You can also build a local binary:

```sh
go build -o talk2text ./cmd/talk2text
```

Then place the binary somewhere on your desktop session's `PATH`, such as `~/.local/bin`.

### Build Release

Add `-ldflags='-s -w' -trimpath` to the install or build command.

## Desktop Integration

Bind `talk2text start` to a key press and `talk2text stop` to the matching key release. Also arrange for exactly one daemon to start with your graphical session.

For example, in Sway:

```text
exec talk2text daemon --out-cmd /home/you/.local/bin/talk2text-copy-clipboard --notify-cmd /home/you/.local/bin/talk2text-notify-send

bindsym --no-repeat F12 exec talk2text start
bindsym --release F12 exec talk2text stop
```

Replace `/home/you` with your home directory. If `talk2text` is not on the `PATH` inherited by Sway, use its full path. Other desktop environments and window managers can use the same press/release command model.

## Configuration

There is currently no configuration file. Common settings are daemon flags:

| Flag | Default | Purpose |
| --- | --- | --- |
| `--whisper-endpoint URL` | `http://127.0.0.1:8080/inference` | Whisper-compatible inference endpoint |
| `--out-cmd PATH` | disabled | Executable that consumes completed transcripts |
| `--notify-cmd PATH` | disabled | Executable that displays lifecycle and error notifications |
| `--http-listen ADDRESS` | disabled | Listen for AMR-WB audio submission over HTTP |
| `--runtime-dir PATH` | discovered automatically | Directory containing the socket, prompt, and transcripts |

Lower-level settings are environment variables read when the daemon starts:

| Variable | Default | Purpose |
| --- | --- | --- |
| `TALK2TEXT_MIN_DURATION` | `500ms` | Clips shorter than this are classified as `short` and are not transcribed; `0s` accepts every non-empty clip |
| `TALK2TEXT_MAX_DURATION` | `100s` | Limit local recordings and HTTP submissions; `0s` disables local auto-stop and cannot be used with HTTP input |
| `TALK2TEXT_STOP_DELAY` | `250ms` | Continue recording briefly after a stop request so the end of speech is not clipped; `0s` stops immediately |
| `TALK2TEXT_WARM_RETENTION` | `15s` | Keep the microphone stream open after a clip for faster repeated dictation; `0s` closes it immediately |
| `TALK2TEXT_TRANSCRIPT_RETENTION_WINDOW` | `100` | Retain transcript files from this many recent clip IDs; `0` disables runtime pruning |
| `TALK2TEXT_RECORD_INPUT_DEVICE` | system default | Exact miniaudio capture-device name or ID |
| `TALK2TEXT_WHISPER_CONNECT_TIMEOUT` | `1s` | Limit for connecting to the Whisper server; `0s` disables it |
| `TALK2TEXT_WHISPER_REQUEST_TIMEOUT` | `10s` | Limit for the complete inference request; `0s` disables it |

Durations use Go-style units, such as `500ms`, `15s`, or `1m40s`. They must be `0s` or at least `10ms`.

For example:

```sh
TALK2TEXT_MIN_DURATION=250ms \
TALK2TEXT_MAX_DURATION=2m \
TALK2TEXT_STOP_DELAY=300ms \
talk2text daemon --whisper-endpoint http://127.0.0.1:8080/inference
```

The daemon acknowledges `stop` immediately but continues collecting audio for the stop delay. If a new `start` arrives during that window, the daemon finishes the previous clip immediately and starts a new clip; the old delayed stop cannot stop the new recording.

If you use `--runtime-dir`, pass the same option to `start`, `stop`, and `status` so that they connect to the same daemon.

## HTTP Audio Submission

Enable the optional HTTP listener with a trusted private-network address or an encrypted tunnel:

```sh
talk2text daemon --http-listen 127.0.0.1:8081
```

Submit a single-channel AMR-WB storage-format stream:

```sh
curl --fail-with-body \
  -H 'Content-Type: audio/amr-wb' \
  --data-binary @recording.amr \
  http://127.0.0.1:8081/transcribe
```

An accepted request returns `202 Accepted` and a daemon-local clip ID. Transcription continues asynchronously through the same Whisper, transcript, output-command, notification, and retention pipeline used by local recordings.

Malformed or unsupported requests are rejected before a clip ID is allocated. HTTP Whisper requests run one at a time, and the daemon returns `503 Service Unavailable` without reading the body when two submissions are already being admitted or waiting to start.

The listener does not provide authentication or TLS and should not be exposed directly to an untrusted network. HTTP input requires a nonzero `TALK2TEXT_MAX_DURATION`, which bounds both the encoded AMR-WB body and decoded PCM duration.

## Whisper Server

The Whisper server is external to `talk2text` and must be started separately. It must accept a multipart WAV upload and return JSON containing a `text` field.

Clips are sent as signed 16-bit mono PCM WAV audio at 16 kHz. Requests also specify JSON response format and may include the transcription prompt described below.

## Transcription Prompt

To send an initial prompt with each Whisper request, create a file named `transcription-prompt` in the active runtime directory. Find that directory with `talk2text status`:

```sh
runtime_dir=$(talk2text status | sed -n 's/^runtime_dir: //p')
printf '%s\n' 'Technical dictation about Go, Linux, and Neovim.' \
  > "$runtime_dir/transcription-prompt"
```

The file is read again for every transcription, so it can be changed without restarting the daemon. If it is absent, no prompt is sent.

## Output Commands

`--out-cmd` can point to any executable. After processing a clip, the daemon invokes it as:

```text
<output-command> <kind> <transcript-path>
```

`<kind>` is one of:

| Kind | Meaning |
| --- | --- |
| `text` | Whisper returned non-empty transcript text |
| `blank` | Whisper returned empty text or `[BLANK_AUDIO]` |
| `short` | The clip did not reach the minimum duration and was not sent to Whisper |

The transcript path points to an owner-only file. It contains cleaned text for `text` and is empty for `blank` or `short`. Output commands may run concurrently and may complete in a different order from the clips.

The output command is responsible for removing the transcript file after successful processing. If it delegates work to another process, it must delay removal until that process has finished reading the file. The included [`talk2text-copy-clipboard`](docs/examples/talk2text-copy-clipboard) command demonstrates this contract.

Without an output command, non-empty transcripts remain in the runtime transcript directory and can be consumed directly.

## Notification Commands

`--notify-cmd` also accepts any executable. The daemon invokes it asynchronously as:

```text
<notification-command> <level> <code> <message>
```

`<level>` is `info` or `error`. Informational event codes include `record-start`, `record-stop`, `transcribe-start`, `transcribe-stop`, and `output-start`. Error codes identify the failing component, such as `audio-capture`, `whisper`, or `output-command`.

See [`talk2text-notify-send`](docs/examples/talk2text-notify-send) for a selective `notify-send` implementation.

## Runtime Directory

Unless overridden, the runtime directory is selected in this order:

1. `$XDG_RUNTIME_DIR/talk2text`
2. `$TMPDIR/run-<uid>/talk2text`
3. `/tmp/run-<uid>/talk2text`

It contains:

1. `daemon.sock`: the Unix socket used by client commands.
2. `transcription-prompt`: the optional Whisper prompt.
3. `transcripts/<clip-id>`: completed transcript files.

Directories and transcript files created by `talk2text` use owner-only permissions. The daemon prunes files outside the configured retention window and removes stale regular transcript files when it next starts.

Local recordings, uploaded AMR-WB bodies, and decoded HTTP audio are held in memory and are not written as temporary audio files. Transcript files can contain sensitive text, so output commands should remove successfully consumed files promptly.

## Commands

```text
talk2text daemon   run the long-lived speech-to-text daemon
talk2text start    begin recording; replaces any active recording
talk2text stop     stop and process the active recording; no-op when idle
talk2text status   print daemon state and effective configuration
```

Client commands use exit status `0` for success, `1` for execution failure, `2` for invalid usage, and `3` when the daemon is unavailable.

Run `talk2text -help` or `talk2text <subcommand> -help` for CLI help.

## Troubleshooting

1. If a client reports `daemon unavailable`, start `talk2text daemon` and make sure the daemon and client use the same runtime directory and environment.
2. If the clipboard does not update, run the daemon in a terminal and inspect stderr. Confirm that one of `wl-copy`, `xclip`, or `xsel` is installed and usable in the graphical session.
3. If transcription fails, confirm the Whisper endpoint is reachable and returns a JSON response with a `text` field. Connection and response details are written to the daemon's stderr.
4. If short clips disappear, remember that the default minimum duration is `500ms`. Lower `TALK2TEXT_MIN_DURATION` if needed.
5. If microphone capture fails, verify the desktop session can record audio. `TALK2TEXT_RECORD_INPUT_DEVICE`, when set, must exactly match a miniaudio device name or ID.
6. If the HTTP listener does not start, confirm `--http-listen` is a valid available address and `TALK2TEXT_MAX_DURATION` is nonzero.

## Contributing

Run the test suite before submitting a change:

```sh
go test ./...
```

The design specification and architecture decisions are under [`docs/`](docs/). Feel free to open GitHub issues for suggestions.
