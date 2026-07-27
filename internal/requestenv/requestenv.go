// Package requestenv validates and applies environment values supplied with
// local and HTTP requests.
package requestenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
)

const (
	// SessionIDName overrides XDGSessionIDName as a recording owner identifier.
	SessionIDName = "TALK2TEXT_SESSION_ID"
	// XDGSessionIDName is the fallback recording owner identifier.
	XDGSessionIDName = "XDG_SESSION_ID"
	// OutputTargetName is an optional user-defined output routing value.
	OutputTargetName = "TALK2TEXT_OUTPUT_TARGET"
	// DisplayName identifies the X display used by clip-specific commands.
	DisplayName = "DISPLAY"
	// WaylandDisplayName identifies the Wayland display used by clip-specific commands.
	WaylandDisplayName = "WAYLAND_DISPLAY"
	// XAuthorityName identifies the X authority file used by clip-specific commands.
	XAuthorityName = "XAUTHORITY"
	// DBusSessionBusAddressName identifies the D-Bus session bus used by clip-specific commands.
	DBusSessionBusAddressName = "DBUS_SESSION_BUS_ADDRESS"
	// XDGRuntimeDirName identifies the session runtime directory used by clip-specific commands.
	XDGRuntimeDirName = "XDG_RUNTIME_DIR"
	// XDGSessionTypeName identifies the graphical session type.
	XDGSessionTypeName = "XDG_SESSION_TYPE"
	// XDGSessionDesktopName identifies the session desktop.
	XDGSessionDesktopName = "XDG_SESSION_DESKTOP"
	// XDGCurrentDesktopName identifies the current desktop.
	XDGCurrentDesktopName = "XDG_CURRENT_DESKTOP"

	maxEncodedEnvironmentBytes = 8 << 10
	maxNameBytes               = 256
	maxValueBytes              = 4 << 10
)

var predefinedNames = map[string]struct{}{
	SessionIDName:             {},
	XDGSessionIDName:          {},
	OutputTargetName:          {},
	DisplayName:               {},
	WaylandDisplayName:        {},
	XAuthorityName:            {},
	DBusSessionBusAddressName: {},
	XDGRuntimeDirName:         {},
	XDGSessionTypeName:        {},
	XDGSessionDesktopName:     {},
	XDGCurrentDesktopName:     {},
}

// ValidateName checks whether name can be represented safely in a process
// environment.
func ValidateName(name string) error {
	switch {
	case name == "":
		return errors.New("environment variable name must not be empty")
	case len(name) > maxNameBytes:
		return fmt.Errorf("environment variable name exceeds %d bytes", maxNameBytes)
	case strings.ContainsAny(name, "=,\x00"):
		return errors.New("environment variable name must not contain '=', comma, or NUL")
	default:
		return nil
	}
}

// ValidateValue checks whether value can be represented safely in a process
// environment.
func ValidateValue(value string) error {
	switch {
	case len(value) > maxValueBytes:
		return fmt.Errorf("environment variable value exceeds %d bytes", maxValueBytes)
	case strings.IndexByte(value, 0) >= 0:
		return errors.New("environment variable value must not contain NUL")
	default:
		return nil
	}
}

// ValidateAllowed checks environment values and the effective request
// allowlist. Additional names must already be validated.
func ValidateAllowed(environment map[string]string, additional []string) error {
	allowed := maps.Clone(predefinedNames)
	for _, name := range additional {
		allowed[name] = struct{}{}
	}

	for name, value := range environment {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("environment variable %s is not allowed", name)
		}
		if err := ValidateValue(value); err != nil {
			return fmt.Errorf("environment variable %s has invalid value: %w", name, err)
		}
	}
	return nil
}

// OriginID returns the request's recording owner identifier.
func OriginID(environment map[string]string) string {
	if value := environment[SessionIDName]; value != "" {
		return value
	}
	return environment[XDGSessionIDName]
}

// Decode parses a JSON object containing environment string values.
func Decode(raw []byte) (map[string]string, error) {
	if len(raw) > maxEncodedEnvironmentBytes {
		return nil, fmt.Errorf("environment object exceeds %d bytes", maxEncodedEnvironmentBytes)
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("invalid environment object: %w", err)
	}
	if encoded == nil {
		return nil, errors.New("environment must be an object")
	}

	environment := make(map[string]string, len(encoded))
	for name, rawValue := range encoded {
		if err := ValidateName(name); err != nil {
			return nil, fmt.Errorf("invalid environment variable name: %w", err)
		}

		var value *string
		if err := json.Unmarshal(rawValue, &value); err != nil || value == nil {
			return nil, fmt.Errorf("environment variable %s must have a string value", name)
		}
		if err := ValidateValue(*value); err != nil {
			return nil, fmt.Errorf("environment variable %s has invalid value: %w", name, err)
		}
		environment[name] = *value
	}
	return environment, nil
}
