# talk2text

## Goal
A minimal push-to-talk speech-to-text tool for Sway on Debian Linux.

While the key is held, audio is recorded with `ffmpeg`. When the key is released, recording stops. The newest usable clip is sent to a local Whisper HTTP server, and the returned text is appended to a transcript file. Optionally, a post-processing command can be run.

## Runtime Model

### Temp Directory
Runtime files are stored under:

- `$XDG_RUNTIME_DIR/talk2text` when `XDG_RUNTIME_DIR` is a existing directory
- otherwise `$TMPDIR/run-<uid>/talk2text` when `TMPDIR` is a existing directory
- otherwise `/tmp/run-<uid>/talk2text`

### Per-Clip Files
Each recording gets a unique clip ID based on the current timestamp plus a short random suffix. A clip may create:

- `<clip_id>.wav`
- `<clip_id>.pid`
- `<clip_id>.ffmpeg.log`
- `<clip_id>.response.json`

### CLI Commands
`talk2text` supports the following commands:

- `record [command...]` Starts recording and optionally runs a post-processing command with the paths of the transcript and context files.
  - The other option is to output the paths which can be then piped into the downstream command. However, given some tasks are run in background, handling stdout may be sophisticated.
- `stop` Stops recording.

### Recorder (`talk2text record [command...]`)
Starts a detached worker that:

- records mono 16 kHz WAV audio with `ffmpeg`
- writes the `ffmpeg` PID to `<clip_id>.pid`
- waits for `ffmpeg` to exit
- returns if `ffmpeg` quits too fast
- validates the WAV with `ffprobe`
- ignores clips shorter than the configured minimum duration
- sends the recorded clip to the Whisper server
- includes a cleaned `prompt` form field from `$temp_dir/transcription-context` when it is not empty
- drops empty transcripts or those containing only `[BLANK_AUDIO]`
- appends the returned text to `$temp_dir/transcript`
- copies the returned text to the clipboard using `wl-copy`
- executes the optional command provided as extra arguments to `record`, appending the path to the transcript file and the context file as the final two arguments
- logs command output to `record.log` and `record.err` in `$temp_dir`
- shows notifications for recording and failures

Notes:
- clean up files when returning early
- don't check for the latest clip anymore because transcripts are appended to a file

### Release Handler (`talk2text stop`)
Scans `*.pid` files, confirms each PID still belongs to the expected `ffmpeg` command for that clip, removes the pid file, and stops the process with escalating signals (`TERM` then `KILL`) if needed. Logs command output to `stop.log` and `stop.err` in `$temp_dir`.

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
- `find`
- `notify-send`
- `wl-copy`

The installer additionally expects `systemctl`, `cmake`, `make`, `git`, and `swaymsg`.

## Non-Goals
- Typing injection
- Streaming transcription
- Multi-language support
- Noise suppression
- Waybar integration
