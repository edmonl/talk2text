# 0002. Use one executable for daemon and client

## Status
Accepted

## Context
The project needs a long-running daemon mode and short-lived shortcut-facing client commands. The original design used separate `talk2textd` and `talk2textctl` executables.

Because this is a small local tool, the daemon and client share runtime directory discovery, socket path logic, command schema, defaults, and versioning. Installing and documenting two binaries adds coordination overhead without much benefit.

## Decision
Build and install a single `talk2text` executable.

The daemon runs as:

```sh
talk2text daemon
```

Shortcut-facing commands run as client subcommands.

## Consequences
Installation, session startup commands, shortcut bindings, and versioning are simpler because there is one binary.

The binary contains both daemon and client code. That is acceptable for this project, but optional target adapters should still be kept behind clear package boundaries so the command remains maintainable as the project grows.
