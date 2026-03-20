# Whisper Push-to-Talk — Phase 1 (Design Specification)

## Overview
A minimal push-to-talk speech-to-text system using a locally hosted Whisper model. Audio is recorded on key hold, transcribed on release, and copied to the clipboard.

Priorities:
- Low latency (persistent model in a long-lived server)
- Predictability (clipboard output)
- Minimal complexity
- Clear feedback via notifications, including generic error signals

---

## Components (Concrete)

1. **Whisper Server**
   - Persistent local HTTP service (localhost)
   - Managed by user `systemd` (auto-start + restart)
   - Model: smaller than medium (e.g., small)
   - Implementation: `whisper.cpp` `whisper-server`
   - Keeps model loaded between requests to avoid per-use startup cost
   - Default port: `9898`
   - Installed binary path: `~/bin/whisper-server`
   - Installed model path: `~/stow/whisper-models/ggml-<model>.bin`

2. **Recorder**
   - Uses PipeWire via `ffmpeg`
   - Records mono, 16 kHz WAV
   - Output: `/tmp/talk2paste-whiper.wav` (overwritten each use)
   - Runs only during key hold
   - Started on key press through a recorder control script
   - Uses a PID file and lock in `$XDG_RUNTIME_DIR/talk2paste`, with `/tmp` as fallback when needed
   - Uses file descriptor `100` to hold the lock on `record.lock`
   - Runs `ffmpeg` with `-nostdin`

3. **Dispatcher**
   - Triggered on key release
   - Takes the same `record.lock` on file descriptor `100`
   - Waits for recorder to fully exit
   - Treats recorder completion as: no live recorder process and cleared `recorder.pid`
   - Validates audio duration
   - Sends WAV via HTTP multipart request → server → receives text
   - Performs minimal cleanup
   - Copies result to clipboard

4. **Clipboard**
   - `wl-copy` (Wayland / Sway)

5. **Notifications (Feedback)**
   - `notify-send`

6. **Keybinding Layer**
   - Managed by Sway
   - Handles press (start) / release (stop + dispatch)

---

## Interaction Flow

Hold Key  
→ Stop any previous recorder process still tracked by PID file  
→ Start recording  
→ Notify: "Recording..."

Release Key  
→ Stop recording with a graceful signal  
→ Wait for recorder to exit  
→ Confirm `recorder.pid` is cleared and no recorder process is still live  
→ Notify: "Transcribing..."  
→ Send audio to server  
→ Receive text  
→ Copy to clipboard  
→ Notify: "Ready: <preview>"

If something fails  
→ Notify: "Something didn't work as expected"

---

## Data Flow

Mic  
→ ffmpeg → /tmp/talk2paste-whiper.wav  
→ Dispatcher → HTTP (localhost)  
→ Whisper Server → text  
→ Clipboard (wl-copy)  
→ Notification preview  

---

## Key Decisions

- Clipboard-only output (no typing)
- Local HTTP interface for the transcription server
- `whisper.cpp` `whisper-server` as the transcription backend
- User `systemd` service for `whisper-server`
- Default HTTP port is `9898`
- Installed binary is copied to `~/bin`
- Installed model is copied to `~/stow/whisper-models`
- Fixed temp file (`/tmp/talk2paste-whiper.wav`)
- No concurrency (single active operation)
- Recorder state is managed with an exact PID file plus a lock
- Recorder lock is taken on `record.lock` via file descriptor `100`
- Dispatcher uses the same lock and PID state as the recorder control path
- Installer manages only the `whisper-server` binary and user service it installs itself
- If an external `whisper-server` already exists, installer must not assume its model path or overwrite its service configuration

### Rationale

- A persistent HTTP server avoids repeated process startup and model load overhead
- This matters most for short push-to-talk clips, where fixed startup cost is a large share of total latency
- `whisper.cpp` keeps the backend simple for Phase 1
- User `systemd` provides restart handling, logging, and lifecycle control suited to a background backend service

---

## Safeguards (Required)

### 1. Single Recorder Guarantee
- Only one recorder process allowed
- Starting a new recording must check the recorder PID file and terminate any still-running recorder before starting another one
- Recorder start and stop operations must share a lock to avoid races between press and release handling

### 2. Recorder Completion Guarantee
- Dispatcher runs only after recorder fully exits
- Ensures WAV file is finalized and readable
- Dispatcher must not infer completion from the WAV file alone
- Dispatcher must hold `record.lock` while checking recorder state before it reads the WAV file
- Recorder stop should send `SIGINT` first and wait for clean exit
- If needed, recorder stop may escalate to `SIGTERM`
- `SIGKILL` is last-resort cleanup only; if used, the WAV file must be discarded and dispatch must not run

### 3. No Overlap Between Record and Dispatch
- While dispatching, new recordings are ignored or blocked
- Prevents file overwrite during read

### 4. Minimum Recording Duration
- Ignore recordings shorter than 1 second

### 5. File Stability Check (Minimal)
- Dispatcher proceeds only if:
  - Dispatcher holds `record.lock` on file descriptor `100`
  - File exists
  - Duration is above threshold
- Recorder PID file has been cleared
- No live recorder process remains for the PID previously tracked in `recorder.pid`

### 6. Server Availability
- Whisper server runs continuously
- Auto-restart handled by user `systemd`
- Dispatcher fails fast with a notification if the server is unavailable

### 7. Managed Installation Boundaries
- Installer may install required OS packages only when missing
- Installer may build and install `whisper-server` only when no managed copy exists
- Installer may create a user service only when it knows the full managed binary path and model path
- Installer must not create or overwrite a service for an external pre-existing `whisper-server`
- Uninstall removes only the copied binary and user service; it does not remove packages, repo clone, or model files

---

## Feedback Design (Notifications)

1. Recording started  
   → Triggered on key press  

2. Transcribing  
   → Triggered after key release  

3. Result ready  
   → Shows short preview of transcribed text  
   → Confirms clipboard is populated  

4. Error  
   → Triggered when recording, transcription, clipboard copy, or server access fails  
   → Uses a generic message such as "Something didn't work as expected"  

5. Busy or stale recorder cleanup  
   → Internal recorder cleanup happens before starting a new recording  
   → No extra user-facing detail is required unless cleanup fails  

---

## System Requirements

Minimum:
- CPU: 4 cores  
- RAM: 4–8 GB  
- GPU: not required  

Recommended:
- CPU: 6–8 cores  
- RAM: 8–16 GB  

---

## Non-Goals (Phase 1)

- No typing injection  
- No streaming transcription  
- No Waybar integration  
- No noise suppression  
- No advanced text formatting  
- No multi-language support  
