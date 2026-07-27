# Request Environment and Output Routing

This document specifies a future extension for preserving the originating graphical environment of local recordings and selecting output handling for sessionless HTTP submissions.

## Goals

The daemon may run once per user while that user has multiple concurrent graphical login sessions. A completed local recording should invoke clip-specific output and notification commands in the environment of the session that started the recording, rather than whichever environment the daemon inherited when it started.

HTTP submissions have no graphical or login-session environment. They may instead provide an abstract output target for the configured output command.

The daemon remains unaware of window managers, editors, clipboards, and the meaning of output target names.

## Local Client Environment

Local client requests should use a newline-delimited JSON protocol. A request may contain an `env` object whose keys and values are environment variable names and values:

```json
{"command":"start","env":{"XDG_SESSION_ID":"3","WAYLAND_DISPLAY":"wayland-1"}}
```

The client should automatically send these variables when they are set:

1. `TALK2TEXT_SESSION_ID`
2. `XDG_SESSION_ID`
3. `DISPLAY`
4. `WAYLAND_DISPLAY`
5. `XAUTHORITY`
6. `DBUS_SESSION_BUS_ADDRESS`
7. `XDG_RUNTIME_DIR`
8. `XDG_SESSION_TYPE`
9. `XDG_SESSION_DESKTOP`
10. `XDG_CURRENT_DESKTOP`

The client should omit an automatically handled variable when it is unset. Users should not need command-line flags for these variables.

The daemon should use `TALK2TEXT_SESSION_ID` as the originating-session identifier when it is present and non-empty. Otherwise, it should use `XDG_SESSION_ID`. These variables remain ordinary members of the request environment.

`TALK2TEXT_SESSION_ID` is an optional override for environments that do not provide a suitable `XDG_SESSION_ID`. A random override must be generated once by the graphical-session launcher and inherited by every client in that session. A client must not generate a new random identifier for each invocation.

The daemon should reject a start request when both session-identifying variables are absent or empty. The rejection should not consume a clip ID.

An accepted `start` environment should be copied into the recording session and remain unchanged for the lifetime of the resulting clip. A later client request must not replace the stored environment of an earlier clip.

## Additional Local Variables

The daemon should support a repeatable option that permits additional environment variables from local clients:

```sh
talk2text daemon \
  --allow-client-env SWAYSOCK \
  --allow-client-env HYPRLAND_INSTANCE_SIGNATURE
```

The `start` client should support a repeatable option that reads and sends additional variables from its current environment:

```sh
talk2text start \
  --send-env SWAYSOCK \
  --send-env HYPRLAND_INSTANCE_SIGNATURE
```

`--send-env` accepts a variable name, not a `NAME=VALUE` assignment. If a requested variable is unset, the client should fail without sending the start request.

The daemon should reject a start request that sends an additional variable not named by `--allow-client-env`. Allowing a variable that the client does not send should leave that variable unset for the clip.

`--allow-client-env` applies only to local Unix-socket clients. It must not permit an HTTP request to send the same variable.

The effective additional-variable allowlist should be included in daemon status output.

## Recording Ownership

The originating-session identifier is routing and ownership metadata, not an authentication credential. Access to the owner-only Unix socket remains the local authorization boundary.

When no recording is active, a valid `start` request establishes the originating session as the recording owner.

While a recording owned by one session is active:

1. A `start` from another session should be rejected as busy without consuming a clip ID or changing the active recording.
2. A `stop` from the owning session should be accepted.
3. A `stop` from another or unidentified session should be a successful no-op.
4. A repeated `start` from the owning session should retain the existing start behavior.

When no recording is active, `stop` should remain a successful no-op.

The `stop` client should automatically send the session-identifying variables. It does not need `--send-env` because output and notification commands use the environment captured by the accepted `start`.

## Command Environment

Before invoking a clip-specific output or notification command for a local recording, the daemon should:

1. Start with its own environment.
2. Remove all automatically handled client variables and all variables named by `--allow-client-env`.
3. Add the variables stored with the clip.

Removing the variables before applying the clip environment prevents a stale value inherited by the daemon from being used when the originating client left that variable unset.

Clip-specific output and notification commands should receive the resulting environment. Notifications and commands without a clip origin may use the daemon environment.

Environment values are process data, not shell syntax. The daemon should pass them directly to the child process without shell expansion or evaluation.

## HTTP Output Target

An HTTP submission has no graphical environment or originating login session. It must not provide session, display, D-Bus, runtime-directory, window-manager, or other variables accepted from local clients.

An HTTP client may provide one output-routing value:

```http
Talk2Text-Env-Output-Target: nvim-work
```

The daemon should map this header to `TALK2TEXT_OUTPUT_TARGET` for the output command of that HTTP clip. The variable should not be added to notification command environments.

The target is an opaque logical name interpreted by the configured output command. It must not be treated as a command line, executable path, or shell fragment. When the header is absent, `TALK2TEXT_OUTPUT_TARGET` should be unset.

The daemon should reject duplicate, malformed, empty, or oversized output target headers before allocating a clip ID. Target values should contain only ASCII letters, digits, `.`, `_`, `:`, or `-`. The exact supported target names belong to the configured output command.

Any other request header beginning with `Talk2Text-Env-` should receive `400 Bad Request`. In particular, `--allow-client-env` must not broaden the HTTP header allowlist.

The HTTP endpoint remains unauthenticated and unencrypted. Output targets should be accepted only under the endpoint's existing trusted-network or user-managed tunnel assumptions.

## Request Validation

The daemon should bound the complete local request and each environment name and value. It should reject malformed JSON, invalid environment names or values, duplicate JSON keys, and requests that exceed the limits. Exact size limits are implementation details.

Client environment values must not be written to normal logs or included in status output. Status may report environment variable names that are configured or active.

Recording ownership ends when recording stops. Transcription and output processing do not keep the originating session busy and retain their existing concurrency behavior.
