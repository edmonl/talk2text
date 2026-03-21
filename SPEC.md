# talk2paste

## Goal
A minimal push-to-talk speech-to-text tool for Sway on Debian Linux.

While the key is held, audio is recorded with `ffmpeg`.
When the key is released, recording stops.
The newest completed clip is transcribed by a local Whisper server and copied to the clipboard.

## Core Design

### Temp Folder
All runtime and temp files live in one folder:

- `$TMPDIR/talk2paste` when `TMPDIR` is set and writable
- otherwise `/tmp/talk2paste`

This folder contains only per-clip runtime files.
No separate runtime directory is used.

### Per-Clip Files
Each recording uses a unique clip ID.
The clip ID must be lexically sortable by creation time.

Example:

`20260320T154501.123456789-ab12cd`

Format:

- wall-clock timestamp
- nanoseconds
- short random suffix for uniqueness

Each clip creates:

- `<clip_id>.wav`
- `<clip_id>.pid`
- `<clip_id>.ffmpeg.log` during recording
- `<clip_id>.response.json` during transcription

`<clip_id>.pid` contains the `ffmpeg` PID for that clip, not the recorder PID.

## Components

### Whisper Server
- Persistent local HTTP service on `127.0.0.1:9898`
- Implemented with `whisper.cpp` `whisper-server`
- Managed by user `systemd`
- Keeps the model loaded between requests

### Recorder
- Started on key press with `talk2paste-record` and no subcommand
- Launches `ffmpeg` to record mono 16 kHz WAV audio
- Creates a unique `<clip_id>.wav`
- Writes the `ffmpeg` PID to `<clip_id>.pid`
- Detaches from the key press path and completes the rest of the lifecycle in the background
- Waits for its own `ffmpeg` process to exit
- After `ffmpeg` exits:
  - if its own `<clip_id>.pid` still exists, removes it and treats any non-zero `ffmpeg` exit as failure
  - verifies that its WAV exists and is readable
  - verifies that its WAV is a valid audio file
  - verifies that its WAV duration is at least 1 second
  - checks whether its clip ID is the newest among all remaining `.wav` files
  - if stale, deletes its own WAV and exits
  - if current, sends the WAV to Whisper
  - deletes the WAV after successful submission
  - treats an empty returned text as failure
  - copies the returned text to the clipboard
  - shows a notification with a short preview

### Release Handler
- Triggered on key release
- Reads all `*.pid` files in the temp folder
- For each pid file:
  - reads the `ffmpeg` PID
  - verifies the PID still belongs to an `ffmpeg` process whose argv matches the expected known prefix and output WAV path
  - sends graceful stop to that `ffmpeg` process
  - deletes the pid file once it has been read

## Runtime Flow

### Key Press
1. Start a recorder instance.
2. Recorder creates a new clip ID.
3. Recorder starts a detached background worker.
4. Worker starts `ffmpeg` writing `<clip_id>.wav`.
5. Worker writes `<clip_id>.pid`.
6. Notify: `Recording...`

### Key Release
1. Release handler reads all `*.pid` files.
2. Release handler validates each PID still matches the expected `ffmpeg` argv.
3. Release handler sends `SIGINT` first.
4. Release handler deletes each pid file after reading it.

### Recorder Completion
1. Recorder waits until its own `ffmpeg` exits.
2. If its own `<clip_id>.pid` still exists, recorder removes it and treats any non-zero `ffmpeg` exit as failure.
3. Recorder validates the resulting WAV.
4. Recorder compares its clip ID to the clip IDs of all remaining `.wav` files.
5. If any newer WAV exists, recorder deletes its own WAV and exits.
6. Otherwise recorder notifies `Transcribing...`
7. Recorder sends the WAV to the Whisper server.
8. After successful submission, recorder deletes its WAV.
9. Recorder extracts the returned text.
10. Recorder copies the text with `wl-copy`.
11. Notify: `Ready: <preview>`

## Ordering Rule
The newest clip wins.

“Newest” is determined only by the clip ID embedded in the filename.
Do not use filesystem timestamps such as `mtime`, `ctime`, or creation time.

## Safeguards

### Process Ownership
Before signaling a PID from `<clip_id>.pid`, the release handler must verify:

- the PID is still live
- the process is `ffmpeg`
- the process argv still matches the expected known `ffmpeg` prefix for talk2paste recording
- shell redirections used when launching `ffmpeg` are not part of this check because they do not appear in `/proc/<pid>/cmdline`

This avoids killing an unrelated process if a PID has been reused.

### Graceful Stop
- Stop recording with `SIGINT` first
- `SIGTERM` is allowed as escalation if needed
- `SIGKILL` is last resort only
- A single configurable stop timeout may be used between escalation steps
- After release handling removes `<clip_id>.pid`, the recorder may still accept and transcribe a WAV produced by a signaled `ffmpeg` if the WAV passes normal validation

### Minimum Clip Length
- Ignore clips shorter than 1 second
- This is a silent drop, not an error notification

### WAV Validation
Before transcription, the recorder must verify:

- the WAV file exists
- the WAV file is readable
- `ffprobe` can parse it successfully
- duration is at least the minimum threshold

If any of these checks fail, the WAV must be discarded and must not be transcribed.
Broken or unreadable WAV output is an error.
Minimum-length failure is not an error.

### Stale Clip Cleanup
- A recorder that loses the “newest clip” check deletes its own WAV and exits
- Release handler deletes pid files after reading them
- Recorder deletes its WAV after successful submission to Whisper
- Recorder deletes per-clip log and response files once they are no longer needed

## Feedback
- On key press: `Recording...`
- Before transcription of the winning clip: `Transcribing...`
- On success: `Ready: <preview>`
- On any failure: `Something didn't work as expected`

## Dependencies
Runtime expects:

- `ffmpeg`
- `ffprobe`
- `curl`
- `jq`
- `wl-copy`
- `notify-send`
- `whisper-server`

Installer may ensure these dependencies exist, but runtime scripts should stay lean.
The installer is Debian-specific for now and may use `apt-get` to install missing packages.

## Non-Goals
- Typing injection
- Streaming transcription
- Multi-language support
- Noise suppression
- Waybar integration
