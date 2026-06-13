package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatCLIErrorPlain(t *testing.T) {
	got := formatCLIError(errors.New("boom"))
	if !strings.Contains(got, "ERROR: boom") {
		t.Fatalf("formatCLIError() = %q, want plain error text", got)
	}
}
