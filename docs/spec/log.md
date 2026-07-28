# Logging

The daemon logs lifecycle events and daemon-owned errors to stderr. It does not create a log file or log routine request, recording, transcription, output, or notification activity.

Errors already returned to a client need not also be logged. Sensitive request-environment values must not appear in logs.
