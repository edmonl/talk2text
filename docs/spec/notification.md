# Notification Command

`--notify-cmd <path>` configures an external notification command, keeping desktop integration outside the daemon. Without it, notifications are suppressed.

For each notification the daemon starts:

```sh
TALK2TEXT_NOTIFY_LEVEL=<info|error> \
TALK2TEXT_NOTIFY_CODE=<code> \
<notification-command> <message>
```

The message is the only argument. Daemon-owned metadata overrides inherited or request-provided values.

Informational codes describe lifecycle events:

1. `record-start`
2. `record-stop`
3. `transcribe-start`
4. `transcribe-stop`
5. `output-start`

Error codes identify a source such as `config`, `runtime`, `audio-capture`, `whisper`, or `output-command`. IPC request errors are returned to the client instead of producing notifications.

Clip-specific messages include the clip ID. Detailed diagnostics belong in stderr logs rather than user-facing messages.

Notification startup or exit failures must not fail recording, transcription, or output handling, and must not trigger recursive notifications.
