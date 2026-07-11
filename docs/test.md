# Manual Test Checklist

These tests are intended to avoid global desktop side effects. Use an isolated runtime directory, a local fake Whisper endpoint, and fake output and notification commands that write only to temporary files.

Assume commands run from the project root. Set `BIN` to the executable under test if it is not `./talk2text`.

## Setup

Goal: create isolated state for daemon tests.

Steps:

1. Build the executable under test.
2. Create a temporary test directory:

   ```sh
   tmp="$(mktemp -d)"
   mkdir -p "$tmp/bin" "$tmp/run"
   export BIN="${BIN:-./talk2text}"
   export TALK2TEXT_TEST_OUT_LOG="$tmp/output.log"
   export TALK2TEXT_TEST_NOTIFY_LOG="$tmp/notify.log"
   ```

3. Create a fake output command:

   ```sh
   cat > "$tmp/bin/out" <<'SH'
   #!/usr/bin/env sh
   kind="$1"
   path="$2"
   text="$(cat "$path" 2>/dev/null || true)"
   printf '%s\t%s\t%s\n' "$kind" "$path" "$text" >> "$TALK2TEXT_TEST_OUT_LOG"
   SH
   chmod +x "$tmp/bin/out"
   ```

4. Create a fake notification command:

   ```sh
   cat > "$tmp/bin/notify" <<'SH'
   #!/usr/bin/env sh
   printf '%s\t%s\t%s\n' "$1" "$2" "$3" >> "$TALK2TEXT_TEST_NOTIFY_LOG"
   SH
   chmod +x "$tmp/bin/notify"
   ```

5. Start a local fake Whisper-compatible HTTP endpoint on loopback. It should accept multipart `POST` requests and return JSON with a `.text` field. For failure tests, restart it with the response needed by the test.

Affected state: files under `$tmp`, child processes started for the daemon and fake Whisper endpoint, and loopback TCP ports.

Cleanup:

1. Stop the daemon and fake Whisper endpoint.
2. Confirm no `talk2text daemon` process from this test remains.
3. Remove `$tmp`.

Risks: if a test hangs, stop the daemon and fake Whisper endpoint from another shell. Do not use real clipboard, notification, editor, or window-manager commands during these tests.

## Daemon Startup and Status

Goal: confirm the daemon starts with isolated runtime state and exposes usable status.

Steps:

1. Start the daemon with explicit isolated paths:

   ```sh
   "$BIN" daemon --runtime-dir "$tmp/run" --whisper-endpoint http://127.0.0.1:<port>/inference --out-cmd "$tmp/bin/out" --notify-cmd "$tmp/bin/notify"
   ```

2. Run:

   ```sh
   "$BIN" status --runtime-dir "$tmp/run"
   ```

3. Confirm status reports `off`, next clip ID `1`, zero pending transcriptions, `$tmp/run` as the runtime directory, and the effective configuration.
4. Confirm `$tmp/run/daemon.sock` and `$tmp/run/transcripts` exist.
5. Stop the daemon with SIGTERM.
6. Confirm `$tmp/run/daemon.sock` was removed.

Affected state: `$tmp/run`, `$tmp/output.log`, `$tmp/notify.log`.

Cleanup: stop the daemon if it is still running.

## Unavailable Daemon

Goal: confirm client commands fail cleanly when no daemon is reachable.

Steps:

1. Ensure no daemon is running for `$tmp/run`.
2. Run:

   ```sh
   "$BIN" start --runtime-dir "$tmp/run"
   ```

3. Confirm it exits with code `3` and prints a short stderr error.
4. Repeat with `stop` and `status`.

Affected state: none beyond command output.

Cleanup: none.

## Short Clip

Goal: confirm clips shorter than the minimum duration are classified as `short` without transcription.

Steps:

1. Start the daemon with the fake commands and fake Whisper endpoint.
2. Run:

   ```sh
   "$BIN" start --runtime-dir "$tmp/run"
   "$BIN" stop --runtime-dir "$tmp/run"
   ```

3. Confirm the fake Whisper endpoint received no request.
4. Confirm `$tmp/output.log` contains one `short` entry.
5. Confirm no `record-stop` or `transcribe-start` notification was emitted for the short clip. A `record-start` notification may or may not be emitted.

Affected state: `$tmp/output.log`, `$tmp/notify.log`, `$tmp/run/transcripts`.

Cleanup: stop the daemon if it is still running.

## Successful Text Clip

Goal: confirm a valid clip is transcribed, written, handed to the output command, and cleaned up after successful output.

Steps:

1. Start the daemon with fake Whisper returning non-empty text.
2. Run `start`, wait longer than the configured minimum duration, then run `stop`.
3. Confirm the fake Whisper endpoint received one multipart `POST` request with the configured form fields.
4. Confirm `$tmp/output.log` contains one `text` entry and the cleaned transcript text.
5. Confirm the transcript file path passed to the output command no longer exists after the output command exits successfully.
6. Confirm notification log contains `record-start`, `record-stop`, `transcribe-start`, `transcribe-stop`, and `output-start` info events.
7. Confirm status eventually reports zero pending transcriptions and the next clip ID incremented.

Affected state: `$tmp/output.log`, `$tmp/notify.log`, `$tmp/run/transcripts`, fake Whisper request log.

Cleanup: stop the daemon if it is still running.

## Blank Clip

Goal: confirm blank transcription is classified as `blank` and still reaches the output command.

Steps:

1. Start the daemon with fake Whisper returning empty text or `[BLANK_AUDIO]`.
2. Record a clip longer than the configured minimum duration.
3. Confirm `$tmp/output.log` contains one `blank` entry with empty transcript text.
4. Confirm the transcript file is removed after the output command exits successfully.

Affected state: `$tmp/output.log`, `$tmp/notify.log`, `$tmp/run/transcripts`.

Cleanup: stop the daemon if it is still running.

## Whisper Failure

Goal: confirm transcription failures do not invoke the output command or leave partial transcript files.

Steps:

1. Start the daemon with fake Whisper returning a non-2xx response, invalid JSON, or JSON without `.text`.
2. Record a clip longer than the configured minimum duration.
3. Confirm the daemon logs the failure.
4. Confirm `$tmp/notify.log` contains an error notification with code `whisper`.
5. Confirm `$tmp/output.log` has no new entry for the failed clip.
6. Confirm no transcript file was created for the failed clip.

Affected state: `$tmp/notify.log`, daemon stderr log, fake Whisper request log.

Cleanup: stop the daemon if it is still running.

## Transcription Context

Goal: confirm optional prompt handling uses isolated files and reports read failures.

Steps:

1. Start the daemon without `$tmp/run/transcription-prompt`.
2. Record a valid clip and confirm the fake Whisper request has no `prompt` field.
3. Create `$tmp/run/transcription-prompt` with prompt text.
4. Record a valid clip and confirm the fake Whisper request includes a cleaned `prompt` field.
5. Replace `$tmp/run/transcription-prompt` with a path that exists but cannot be read.
6. Record a valid clip and confirm the daemon logs an error, emits an error notification, and does not invoke the output command for that clip.

Affected state: `$tmp/run/transcription-prompt`, fake Whisper request log, `$tmp/output.log`, `$tmp/notify.log`.

Cleanup: restore or remove `$tmp/run/transcription-prompt`; stop the daemon if it is still running.

## Output Command Failure

Goal: confirm output command failures preserve transcript files for inspection.

Steps:

1. Replace the fake output command with one that exits non-zero.
2. Start the daemon and record a valid text clip.
3. Confirm the daemon logs the output failure.
4. Confirm `$tmp/notify.log` contains an error notification with code `output-command`.
5. Confirm the transcript file for the clip still exists and contains the transcript text.

Affected state: `$tmp/run/transcripts`, `$tmp/notify.log`, daemon stderr log.

Cleanup: stop the daemon if it is still running; remove `$tmp` when inspection is complete.

## Notification Command Failure

Goal: confirm notification failures do not break recording, transcription, or output.

Steps:

1. Replace the fake notification command with one that exits non-zero.
2. Start the daemon and record a valid text clip.
3. Confirm transcription and output still complete successfully.
4. Confirm the daemon logs notification command failures without recursive notification attempts.

Affected state: daemon stderr log, `$tmp/output.log`, `$tmp/run/transcripts`.

Cleanup: stop the daemon if it is still running.

## Runtime Path Safety

Goal: confirm unsafe or unsuitable runtime paths fail without deleting unrelated files.

Steps:

1. Create a regular file at `$tmp/not-a-dir`.
2. Run the daemon with `--runtime-dir "$tmp/not-a-dir"` and confirm startup fails.
3. Create `$tmp/run/transcripts` as a regular file and confirm daemon startup fails.
4. Create an extra regular file directly under `$tmp/run/transcripts`, start the daemon, and confirm startup removes that stale transcript file.
5. Create a subdirectory under `$tmp/run/transcripts`, start the daemon, and confirm startup does not recursively delete it.

Affected state: files under `$tmp`.

Cleanup: remove `$tmp` after inspection.

## Existing Daemon Socket

Goal: confirm daemon startup handles existing socket paths correctly.

Steps:

1. Start one daemon with `--runtime-dir "$tmp/run"`.
2. Start a second daemon with the same runtime directory and confirm it fails because a daemon is already running.
3. Stop the first daemon uncleanly if needed, leaving a stale socket.
4. Start the daemon again and confirm it removes the stale socket and starts successfully.
5. Replace `daemon.sock` with a regular file and confirm daemon startup fails without deleting it.

Affected state: `$tmp/run/daemon.sock`, daemon processes.

Cleanup: stop any daemon from this test.

## Maximum Duration

Goal: confirm the daemon auto-stops and processes a clip at the configured maximum duration.

Steps:

1. Start the daemon with a short test-only maximum duration.
2. Run `start` and do not run `stop`.
3. Confirm the daemon automatically processes the clip after the configured maximum.
4. Confirm status returns to idle or warm state and pending transcriptions eventually returns to zero.

Affected state: `$tmp/output.log`, `$tmp/notify.log`, `$tmp/run/transcripts`.

Cleanup: stop the daemon if it is still running.

## Shutdown

Goal: confirm shutdown releases daemon-owned resources without intentionally killing already-started external commands.

Steps:

1. Replace the fake output command with one that records its start and sleeps.
2. Start the daemon and record a valid clip so the output command starts.
3. Send SIGTERM to the daemon while the output command is still sleeping.
4. Confirm the daemon exits promptly and removes `daemon.sock`.
5. Confirm the already-started output command is not intentionally killed by the daemon.

Affected state: daemon process, output command process, `$tmp/run`, `$tmp/output.log`.

Cleanup: stop the sleeping output command if it remains after the check.

## Real Audio Smoke Test

Goal: confirm microphone capture works on the target desktop while keeping other side effects isolated.

Steps:

1. Use the same isolated runtime directory, fake output command, fake notification command, and local fake or disposable Whisper endpoint.
2. Record one short spoken phrase.
3. Confirm the output log contains the expected classification and transcript text.

Affected state: microphone device access during the recording window and files under `$tmp`.

Cleanup: stop the daemon and remove `$tmp`.

Risks: this is the only checklist item that touches real audio input. It should not change global audio configuration.
