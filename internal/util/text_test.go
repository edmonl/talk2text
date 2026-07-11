package util

import "testing"

func TestCollapseSpace(t *testing.T) {
	got := CollapseSpace(" hello\n\nworld\t ")
	if got != "hello world" {
		t.Fatalf("CollapseSpace = %q, want hello world", got)
	}
}
