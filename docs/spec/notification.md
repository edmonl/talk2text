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

Notification messages should start with an uppercase letter.

Clip-specific notification messages, including both informational and error notifications, should include the daemon-local clip ID.

For `info` notifications, `<code>` identifies the lifecycle event. Event codes are:

1. `record-start`
2. `record-stop`
3. `transcribe-start`
4. `transcribe-stop`
5. `output-start`

For `error` notifications, `<code>` identifies the error source, such as `config`, `runtime`, `audio-capture`, `whisper`, or `output-command`. IPC request errors are returned to the client and should not emit user notifications.

If an error happens in a user-triggered process and may prevent the user from seeing the result, the daemon should emit an error notification.

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

The daemon does not impose a timeout on notification commands during normal operation. Notification command startup failures should be logged but should not fail recording, transcription, or output handling. Notification command failures must not trigger another notification attempt. The daemon should reap notification command processes in the background. Later exit failures may also be logged.
