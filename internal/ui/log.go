// Package ui renders the CLI output: log lines now, tables later. Colors are
// dropped when the output is not a terminal.
package ui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// Logger writes the CLI log lines, in the format of earlier releases, all
// on the same writer (stdout):
//
//	info     "-- <msg>"        "--" in blue
//	success  "-- <msg>"        "--" in green
//	warning  "WARNING: <msg>"  whole line in yellow
//	error    "ERROR: <msg>"    whole line in red
type Logger struct {
	w                        io.Writer
	blue, green, yellow, red lipgloss.Style
}

// NewLogger returns a Logger writing to w, with colors only if w is a
// terminal.
func NewLogger(w io.Writer) *Logger {
	r := lipgloss.NewRenderer(w)
	return &Logger{
		w:      w,
		blue:   r.NewStyle().Foreground(lipgloss.ANSIColor(4)),
		green:  r.NewStyle().Foreground(lipgloss.ANSIColor(2)),
		yellow: r.NewStyle().Foreground(lipgloss.ANSIColor(3)),
		red:    r.NewStyle().Foreground(lipgloss.ANSIColor(1)),
	}
}

// Info logs an informational message.
func (l *Logger) Info(msg string) { fmt.Fprintln(l.w, l.blue.Render("--")+" "+msg) }

// Success logs a success message.
func (l *Logger) Success(msg string) { fmt.Fprintln(l.w, l.green.Render("--")+" "+msg) }

// Warning logs a warning.
func (l *Logger) Warning(msg string) { fmt.Fprintln(l.w, l.yellow.Render("WARNING: "+msg)) }

// Error logs an error.
func (l *Logger) Error(msg string) { fmt.Fprintln(l.w, l.red.Render("ERROR: "+msg)) }
