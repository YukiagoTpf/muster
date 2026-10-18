// Package picker is the interactive chooser behind a bare `muster`: a list
// screen driven by arrow keys (or j/k, or a digit), rendered in place in the
// terminal. Screen is pure — keys in, state and lines out — so it is tested
// without a terminal; Terminal does the raw-mode I/O.
package picker

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Item is one row of a screen.
type Item struct {
	Label  string // the thing chosen, e.g. a credential name
	Quota  string // a styled quota cell, aligned in its own column
	Detail string // dim context after the label
	Tag    string // a short marker such as "last"
}

// Screen is one list to choose from.
type Screen struct {
	Title  string
	Items  []Item
	Cursor int
	// CanGoBack enables Esc / ← as "back".
	CanGoBack bool
}

// Key is one decoded keypress.
type Key int

const (
	KeyNone Key = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyBack
	KeyQuit
	Key1 // Key1 + n selects item n+1 directly
)

// Keys decodes the bytes of one terminal read. A lone ESC is "back"; ESC
// starting a CSI sequence is an arrow key.
func Keys(b []byte) []Key {
	var keys []Key
	for len(b) > 0 {
		switch {
		case len(b) >= 3 && b[0] == 0x1b && (b[1] == '[' || b[1] == 'O'):
			switch b[2] {
			case 'A':
				keys = append(keys, KeyUp)
			case 'B':
				keys = append(keys, KeyDown)
			case 'D':
				keys = append(keys, KeyBack)
			case 'C':
				keys = append(keys, KeyEnter)
			}
			b = b[3:]
			continue
		case b[0] == 0x1b:
			keys = append(keys, KeyBack)
		case b[0] == 'k' || b[0] == 0x10: // k, Ctrl-P
			keys = append(keys, KeyUp)
		case b[0] == 'j' || b[0] == 0x0e: // j, Ctrl-N
			keys = append(keys, KeyDown)
		case b[0] == '\r' || b[0] == '\n':
			keys = append(keys, KeyEnter)
		case b[0] == 'q' || b[0] == 0x03 || b[0] == 0x04: // q, Ctrl-C, Ctrl-D
			keys = append(keys, KeyQuit)
		case b[0] >= '1' && b[0] <= '9':
			keys = append(keys, Key1+Key(b[0]-'1'))
		}
		b = b[1:]
	}
	return keys
}

// Outcome is how a screen ended.
type Outcome int

const (
	Pending Outcome = iota // still choosing
	Chosen                 // Items[Cursor] was chosen
	Back
	Quit
)

// Handle applies one key and reports whether the screen is done.
func (s *Screen) Handle(k Key) Outcome {
	switch {
	case k == KeyUp && s.Cursor > 0:
		s.Cursor--
	case k == KeyDown && s.Cursor < len(s.Items)-1:
		s.Cursor++
	case k == KeyEnter && len(s.Items) > 0:
		return Chosen
	case k == KeyBack && s.CanGoBack:
		return Back
	case k == KeyQuit:
		return Quit
	case k >= Key1 && int(k-Key1) < len(s.Items):
		s.Cursor = int(k - Key1)
		return Chosen
	}
	return Pending
}

const (
	bold   = "\x1b[1m"
	cyan   = "\x1b[1;36m"
	dim    = "\x1b[2m"
	yellow = "\x1b[33m"
	reset  = "\x1b[0m"
)

// Lines renders the screen, each line cut to width columns so none wraps
// (a wrapped line would break the in-place redraw).
func (s Screen) Lines(width int) []string {
	hint := "↑↓ move · enter launch · q quit"
	if s.CanGoBack {
		hint = "↑↓ move · enter launch · esc back · q quit"
	}
	lines := []string{fit(bold+s.Title+reset+"  "+dim+hint+reset, width)}
	// The tag sits right after the label, so a narrow terminal cuts the
	// detail, never the tag.
	head := func(it Item) int {
		n := utf8.RuneCountInString(it.Label)
		if it.Tag != "" {
			n += utf8.RuneCountInString(it.Tag) + 3
		}
		return n
	}
	headWidth, quotaWidth := 0, 0
	for _, it := range s.Items {
		headWidth = max(headWidth, head(it))
		quotaWidth = max(quotaWidth, visible(it.Quota))
	}
	for i, it := range s.Items {
		marker, label := "  ", it.Label
		if i == s.Cursor {
			marker, label = cyan+"❯ ", cyan+it.Label+reset
		}
		if it.Tag != "" {
			label += " " + yellow + "(" + it.Tag + ")" + reset
		}
		line := marker + label + strings.Repeat(" ", headWidth-head(it))
		if quotaWidth > 0 {
			line += "  " + it.Quota + strings.Repeat(" ", quotaWidth-visible(it.Quota))
		}
		lines = append(lines, fit(fmt.Sprintf("%s  %s%s%s", line, dim, it.Detail, reset), width))
	}
	return lines
}

// visible is the number of columns s takes, ANSI escapes excluded.
func visible(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if end := strings.IndexByte(s[i:], 'm'); end >= 0 {
				i += end + 1
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		n++
		i += size
	}
	return n
}

// fit cuts s to width visible columns, skipping ANSI escapes when counting
// and closing any open style.
func fit(s string, width int) string {
	if width <= 0 {
		return s
	}
	var b strings.Builder
	cols := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			end := strings.IndexByte(s[i:], 'm')
			if end < 0 {
				break
			}
			b.WriteString(s[i : i+end+1])
			i += end + 1
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if cols+1 > width {
			b.WriteString(reset)
			return b.String()
		}
		b.WriteRune(r)
		cols++
		i += size
	}
	return b.String()
}
