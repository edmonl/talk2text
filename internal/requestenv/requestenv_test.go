package requestenv

import (
	"strings"
	"testing"
)

func TestDecodeRejectsInvalidEnvironmentObjects(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "not object", raw: `[]`, want: "cannot unmarshal array"},
		{name: "non-string", raw: `{"DISPLAY":7}`, want: "environment variable DISPLAY must have a string value"},
		{name: "empty name", raw: `{"":"value"}`, want: "name must not be empty"},
		{name: "equals in name", raw: `{"A=B":"value"}`, want: "must not contain '=', comma, or NUL"},
		{name: "comma in name", raw: `{"A,B":"value"}`, want: "must not contain '=', comma, or NUL"},
		{name: "nul in value", raw: `{"DISPLAY":"a\u0000b"}`, want: "value must not contain NUL"},
		{name: "trailing value", raw: `{"DISPLAY":"one"} {}`, want: "after top-level value"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestDecodeDuplicateEnvironmentUsesLastValue(t *testing.T) {
	environment, err := Decode([]byte(`{"DISPLAY":"one","DISPLAY":"two"}`))
	if err != nil {
		t.Fatal(err)
	}
	if environment[DisplayName] != "two" {
		t.Fatalf("DISPLAY = %q, want two", environment[DisplayName])
	}
}

func TestValidateAllowedUsesPredefinedAndAdditionalNames(t *testing.T) {
	environment := map[string]string{
		XDGSessionIDName: "3",
		OutputTargetName: "mobile",
		"SWAYSOCK":       "socket",
	}
	if err := ValidateAllowed(environment, []string{"SWAYSOCK"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAllowed(environment, nil); err == nil || !strings.Contains(err.Error(), "SWAYSOCK is not allowed") {
		t.Fatalf("ValidateAllowed error = %v, want disallowed SWAYSOCK", err)
	}
}
