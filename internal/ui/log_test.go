package ui

import (
	"bytes"
	"testing"
)

// A buffer is not a terminal: the lines come out without escape codes.
func TestLoggerPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf)
	l.Info("starting")
	l.Success("done")
	l.Warning("careful")
	l.Error("failed")

	want := "-- starting\n-- done\nWARNING: careful\nERROR: failed\n"
	if got := buf.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
