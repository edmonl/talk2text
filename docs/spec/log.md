# Logging

The daemon logs to stderr. It should not write a daemon log file during normal operation.

The daemon should log daemon-owned errors, except where another spec section explicitly makes logging optional or unnecessary.

If an error is returned to the client, the daemon does not need to log it.

Informational logs should be limited to key daemon lifecycle events during startup and shutdown. The daemon should not log every client request, recording, transcription, output command, or notification event merely for informational tracing.

Log messages should be lowercase and should not end with punctuation.

Production formatting should not use `%v`; tests may use `%v`.
