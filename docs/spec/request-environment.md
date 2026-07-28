# Request Environment and Output Routing

Request environments let one daemon serve multiple graphical sessions while running clip-specific output and notification commands in the environment that started each clip. The daemon treats these values as opaque routing data.

## Local Requests

`start` and `stop` requests may contain an `env` object of environment variable names and string values:

```json
{"command":"start","env":{"XDG_SESSION_ID":"3","WAYLAND_DISPLAY":"wayland-1"}}
```

Predefined allowed names:

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

The `start` client sends set predefined variables except `TALK2TEXT_OUTPUT_TARGET`. The `stop` client sends only the two session identifiers. Unset variables are omitted, and `status` sends no environment.

`TALK2TEXT_SESSION_ID` is the preferred recording-owner identifier; a non-empty `XDG_SESSION_ID` is the fallback. A session launcher may provide a stable `TALK2TEXT_SESSION_ID` when the desktop does not provide a useful XDG session ID.

A start without either identifier is rejected without consuming a clip ID. An accepted start environment is captured for that clip and cannot be replaced by later requests.

## Additional Variables

The daemon accepts repeatable `--allow-client-env <name>` options for additional local and HTTP variables. The `start` client reads additional variables through repeatable `--send-env <name>` options. `stop` does not accept `--send-env`.

`--send-env` takes a variable name, not an assignment, and omits an unset variable. Requests containing names outside the effective allowlist are rejected. The additional allowlist appears in daemon status.

## Recording Ownership

The session identifier is routing metadata, not an authentication credential; the owner-only Unix socket is the local authorization boundary.

When a recording is active:

1. Another owner’s `start` is rejected as busy without changing clip state.
2. The owner’s `stop` is accepted.
3. Another or unidentified owner’s `stop` succeeds without effect.
4. The owner’s repeated `start` keeps the normal replacement behavior.

Stopping while idle also succeeds without effect. Ownership ends when recording stops; later transcription and output do not keep the session busy.

## Command Environment

Clip-specific output and notification commands start with the daemon environment overlaid by the captured request environment. Variables absent from the request retain daemon values.

The daemon then applies command-owned metadata, which takes precedence:

1. Output commands: `TALK2TEXT_OUTPUT_KIND`, `TALK2TEXT_NOTIFY_CMD`
2. Notification commands: `TALK2TEXT_NOTIFY_LEVEL`, `TALK2TEXT_NOTIFY_CODE`

Values are passed directly as process environment data without shell evaluation.

## HTTP Requests

An HTTP submission may provide one JSON environment object:

```http
Talk2Text-Env: {"TALK2TEXT_OUTPUT_TARGET":"mobile"}
```

The same syntax and allowlist apply to local and HTTP environments. Duplicate, malformed, or disallowed headers receive `400 Bad Request` before a clip ID is allocated.

HTTP environments provide command routing only; they do not establish recording ownership. Because the endpoint has no built-in authentication or encryption, environment values are safe only under its trusted-network assumptions.

## Validation

The first complete local JSON value is the request; later bytes are ignored. Requests require a string `command`, reject unknown top-level fields, and allow only string-valued environment objects. `status` rejects an environment object.

Environment names must be non-empty and contain no `=`, comma, or NUL. Values must contain no NUL. Empty values are allowed but do not establish ownership.

Local requests, HTTP environment headers, names, and values are bounded. Environment values must not appear in normal logs or status output.
