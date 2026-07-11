// Package util contains small shared helpers.
package util

import (
	"strings"
	"unicode"
)

// CollapseSpace trims text and replaces each run of whitespace with a single space.
func CollapseSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// UpperFirst makes the first letter uppercase.
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
