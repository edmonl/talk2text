# talk2paste

## Goal
A minimal push-to-talk speech-to-text tool for Sway on Debian Linux.

While the key is held, audio is recorded with `ffmpeg`. When the key is released, recording stops. The newest usable clip is sent to a local Whisper HTTP server, and the returned text is copied to the Wayland clipboard.

## Runtime Model

### Temp Directory
Runtime files are stored under:

- `$TMPDIR/talk2paste` when `TMPDIR` exists and is writable
- otherwise `/tmp/talk2paste`

### Per-Clip Files
Each recording gets a unique clip ID based on the current timestamp plus a short random suffix. A clip may create:

- `<clip_id>.wav`
- `<clip_id>.pid`
- `<clip_id>.ffmpeg.log`
- `<clip_id>.response.json`

### Recorder
`talk2paste-record` starts a detached worker that:

- records mono 16 kHz WAV audio with `ffmpeg`
- writes the `ffmpeg` PID to `<clip_id>.pid`
- waits for `ffmpeg` to exit
- drops stale clips when a newer `.wav` already exists
- validates the WAV with `ffprobe`
- ignores clips shorter than the configured minimum duration
- sends the winning clip to the Whisper server
- includes a cleaned `prompt` form field from the selection clipboard when it is not empty
- copies the returned text with `wl-copy`
- shows notifications for recording, transcription, success, and failures

### Release Handler
`talk2paste-stop` scans `*.pid` files, confirms each PID still belongs to the expected `ffmpeg` command for that clip, removes the pid file, and stops the process with escalating signals if needed.

## Ordering Rule
The newest clip wins.

Newness is determined by the clip ID in the filename, not by filesystem timestamps.

## Whisper Server
The runtime expects an HTTP endpoint at `http://127.0.0.1:9898/inference` by default. The installer provisions `whisper.cpp` `whisper-server` as a user `systemd` service and keeps the model loaded between requests.

## Installer
`install` supports:

- `--install` to build or update the managed `whisper-server`, install the user service, and copy the runtime scripts into `~/bin`
- `--uninstall` to remove the runtime scripts and, when the managed `~/bin/whisper-server` exists, remove that binary, the user service, and local `whisper.cpp` build artifacts

The installer leaves the repository checkout and downloaded model files in place.

## Runtime Dependencies
Runtime scripts expect:

- `ffmpeg`
- `ffprobe`
- `curl`
- `jq`
- `wl-copy`
- `wl-paste`
- `notify-send`

The installer additionally expects `systemctl`, `cmake`, `make`, and `git`.

## Non-Goals
- Typing injection
- Streaming transcription
- Multi-language support
- Noise suppression
- Waybar integration
