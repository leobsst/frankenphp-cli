package ui

import (
	"bytes"
	"strings"
	"testing"
)

// A fixed-width column cuts long cells with an ellipsis, one line per row:
// `status` shows long domains this way.
func TestFixedWidthColumnTruncates(t *testing.T) {
	c := NewConsole(&bytes.Buffer{})
	out := c.Render(Table{
		Columns: []Column{{Header: "Domain", Width: 10}, {Header: "PHP"}},
		Rows:    [][]string{{"https://very-long-domain-name.test", "8.3"}, {"https://a", "8.4"}},
	})
	want := `╭────────────┬─────╮
│ Domain     │ PHP │
├────────────┼─────┤
│ https://v… │ 8.3 │
│ https://a  │ 8.4 │
╰────────────┴─────╯`
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if strings.Count(out, "\n") != 5 {
		t.Error("a row was wrapped")
	}
}
