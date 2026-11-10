package ui

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
)

// Colors of the CLI output, the standard ANSI colors.
const (
	Red     = lipgloss.ANSIColor(1)
	Green   = lipgloss.ANSIColor(2)
	Yellow  = lipgloss.ANSIColor(3)
	Blue    = lipgloss.ANSIColor(4)
	Magenta = lipgloss.ANSIColor(5)
	Cyan    = lipgloss.ANSIColor(6)
)

// Console writes styled text and tables, without escape codes when w is
// not a terminal.
type Console struct {
	w io.Writer
	r *lipgloss.Renderer
}

// NewConsole returns a Console writing to w.
func NewConsole(w io.Writer) *Console {
	return &Console{w: w, r: lipgloss.NewRenderer(w)}
}

// Println writes the strings, then a newline.
func (c *Console) Println(s ...string) { fmt.Fprintln(c.w, strings.Join(s, "")) }

// Bold renders s in bold.
func (c *Console) Bold(s string) string { return c.r.NewStyle().Bold(true).Render(s) }

// Dim renders s faint.
func (c *Console) Dim(s string) string { return c.r.NewStyle().Faint(true).Render(s) }

// Color renders s in color.
func (c *Console) Color(color lipgloss.TerminalColor, s string) string {
	return c.r.NewStyle().Foreground(color).Render(s)
}

// Column is a table column. Width is the content width, 0 to fit (longer
// cells are cut with an ellipsis); Color
// colors the whole column, Faint dims it.
type Column struct {
	Header string
	Width  int
	Color  lipgloss.TerminalColor
	Faint  bool
}

// Table is a table with an optional title, centered above it.
type Table struct {
	Title   string
	Columns []Column
	Rows    [][]string
}

// Render returns the table, its rows already styled or plain.
func (c *Console) Render(t Table) string {
	headers := make([]string, len(t.Columns))
	for i, col := range t.Columns {
		headers[i] = col.Header
	}
	// A fixed-width column cuts what overflows with an ellipsis, one line
	// per row
	rows := make([][]string, len(t.Rows))
	for r, row := range t.Rows {
		rows[r] = slices.Clone(row)
		for i, cell := range row {
			if i < len(t.Columns) && t.Columns[i].Width > 0 {
				rows[r][i] = ansi.Truncate(cell, t.Columns[i].Width, "…")
			}
		}
	}
	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(c.r.NewStyle()).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			s := c.r.NewStyle().Padding(0, 1)
			column := t.Columns[col]
			if column.Width > 0 {
				s = s.Width(column.Width + 2)
			}
			if row == table.HeaderRow {
				return s.Bold(true)
			}
			if column.Color != nil {
				s = s.Foreground(column.Color)
			}
			if column.Faint {
				s = s.Faint(true)
			}
			return s
		})
	out := tbl.Render()
	if t.Title == "" {
		return out
	}
	width := lipgloss.Width(strings.SplitN(out, "\n", 2)[0])
	title := lipgloss.PlaceHorizontal(width, lipgloss.Center, c.r.NewStyle().Italic(true).Render(t.Title))
	return strings.TrimRight(title, " ") + "\n" + out
}

// PrintTable writes the table.
func (c *Console) PrintTable(t Table) { c.Println(c.Render(t)) }
