# 0003. Use separate start and stop commands

## Status
Accepted

## Context
The primary interaction model is push-to-talk: pressing a shortcut starts recording, and releasing it stops recording. The CLI could expose either one state-dependent `toggle` command or separate `start` and `stop` commands.

A single `toggle` command is concise and works in environments that only support key-press shortcuts, but it depends on current daemon state. Missed events, duplicate events, or accidental repeats can make the next command do the opposite of what the user intended.

Separate commands make the caller's intent explicit and match press/release desktop bindings.

## Decision
Use separate `talk2text start` and `talk2text stop` commands for the primary recording workflow.

`talk2text start` starts a new recording session. If a recording is already active, it discards the active session without transcription and starts a new one.

`talk2text stop` stops and processes the active recording session. If no recording is active, it is a no-op.

## Consequences
Push-to-talk bindings are explicit and easy to reason about:

```sh
bindsym --no-repeat F12 exec talk2text start
bindsym --release F12 exec talk2text stop
```

Repeated `stop` commands are harmless. Repeated `start` commands intentionally reset the active recording, so desktop bindings should suppress key repeat where possible.

A future `toggle` command can still be added for tap-to-start/tap-to-stop environments, but it is not the primary workflow.
