# Request Environment and Output Routing

This document specifies how local and HTTP requests provide environment values for clip-specific output and notification commands.

## Goals

The daemon may run once per user while that user has multiple concurrent graphical login sessions. A completed local recording should invoke clip-specific output and notification commands in the environment of the session that started the recording, rather than whichever environment the daemon inherited when it started.

HTTP submissions have no recording ownership or login-session semantics, but they may provide allowed environment values for clip-specific commands.

The daemon remains unaware of window managers, editors, clipboards, and the meaning of user-provided routing values.

## Local Client Environment

Local client requests use a JSON protocol. An `env` object on a `start` or `stop` request contains environment variable names and values:

```json
{"command":"start","env":{"XDG_SESSION_ID":"3","WAYLAND_DISPLAY":"wayland-1"}}
```

```json
{"command":"stop","env":{"XDG_SESSION_ID":"3"}}
```

The predefined request environment allowlist contains:

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
11. `TALK2TEXT_OUTPUT_TARGET`

The `start` client should automatically send the predefined variables other than `TALK2TEXT_OUTPUT_TARGET` when they are set. The `stop` client should automatically send only the session-identifying variables when they are set. Automatically handled variables should be omitted when unset. The `status` client should not send an environment object.

The daemon should use `TALK2TEXT_SESSION_ID` as the originating-session identifier when it is present and non-empty. Otherwise, it should use `XDG_SESSION_ID`. These variables remain ordinary members of the request environment.

`TALK2TEXT_SESSION_ID` is an optional override for environments that do not provide a suitable `XDG_SESSION_ID`. A random override must be generated once by the graphical-session launcher and inherited by every client in that session. A client must not generate a new random identifier for each invocation.

The daemon should reject a start request when both session-identifying variables are absent or empty. The rejection should not consume a clip ID.

An accepted `start` environment should be copied into the recording session and remain unchanged for the lifetime of the resulting clip. A later client request must not replace the stored environment of an earlier clip.

## Additional Variables

The daemon should support a repeatable option that permits additional environment variables from local and HTTP requests:

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

`--send-env` accepts a variable name, not a `NAME=VALUE` assignment. A requested variable is omitted when it is unset.

The effective request environment allowlist consists of the predefined variables and variables named by `--allow-client-env`. The daemon should apply the same allowlist to `start`, `stop`, and HTTP request environments. A request that sends any other variable should be rejected.

Request validation should not reject an allowed variable merely because the receiving command does not use it. Recording ownership uses only the session-identifying variables. Other allowed variables in a `stop` request are ignored. HTTP processing does not use session-identifying variables.

The bundled client should send only variables useful to its command. In particular, `stop` does not support `--send-env`.

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

The `stop` client should automatically send the session-identifying variables. A `stop` request without a non-empty session identifier should be a successful no-op.

## Command Environment

Before invoking a clip-specific output or notification command, the daemon should start with its own environment and overlay the environment values supplied for the clip. Variables absent from the clip environment retain their inherited daemon values.

Clip-specific output and notification commands should receive the resulting environment. Notifications and commands without a clip origin may use the daemon environment.

After applying the request environment, the daemon should set command-specific metadata:

1. Output commands receive `TALK2TEXT_OUTPUT_KIND` and `TALK2TEXT_NOTIFY_CMD`.
2. Notification commands receive `TALK2TEXT_NOTIFY_LEVEL` and `TALK2TEXT_NOTIFY_CODE`.

These variables are daemon-owned for the commands where the daemon generates them. Their generated values take precedence over identically named variables inherited from the daemon or supplied with a request. The names do not need to be prohibited from the request environment, but users must not rely on a supplied value being preserved when the daemon generates that variable.

Environment values are process data, not shell syntax. The daemon should pass them directly to the child process without shell expansion or evaluation.

## HTTP Request Environment

An HTTP client may provide an environment object in a single `Talk2Text-Env` header:

```http
Talk2Text-Env: {"TALK2TEXT_OUTPUT_TARGET":"mobile"}
```

The header is optional. When it is absent, the request does not override the daemon environment. `TALK2TEXT_OUTPUT_TARGET` is an optional convention interpreted by the configured output command; the daemon assigns it no special meaning. For example, an output command may use the value `mobile` to handle an HTTP submission differently from a local recording.

The daemon should apply the same environment syntax and allowlist validation to the header object as it applies to local request environments. A duplicate or malformed `Talk2Text-Env` header should receive `400 Bad Request` before a clip ID is allocated.

The HTTP endpoint remains unauthenticated and unencrypted. Request environment values should be accepted only under the endpoint's existing trusted-network or user-managed tunnel assumptions. Users are responsible for the effects of variables they add through `--allow-client-env`.

## Request Validation

The first complete JSON value received from a local client is the request, and any bytes after that value are ignored. The request must be an object with a required string `command`. An `env` object must contain string names and values. A `status` request must not contain an `env` object. Whether `start` and `stop` require an `env` object, and how missing, null, and empty environments are distinguished, are implementation details. Unknown top-level fields should be rejected. Handling duplicate JSON object keys and character encoding accepted by the JSON parser are also implementation details.

Environment names must be non-empty and must not contain `=`, comma, or NUL. Environment values must not contain NUL. No other character restrictions apply. Empty values are permitted, but an empty session identifier does not establish recording ownership.

The daemon should bound the local bytes through the end of the first JSON value, the complete HTTP environment header, and each environment name and value. Requests that exceed the limits should be rejected. Trailing local bytes are ignored and do not count toward the request size. A complete invalid first local JSON value should receive an unsuccessful JSON response. Exact size limits and handling of incomplete requests are implementation details.

Client environment values must not be written to normal logs or included in status output. Status may report environment variable names that are configured or active.

Recording ownership ends when recording stops. Transcription and output processing do not keep the originating session busy and retain their existing concurrency behavior.
