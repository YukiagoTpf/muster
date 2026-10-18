package picker

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/YukiagoTpf/muster/internal/platform"
)

// Interactive reports whether both ends are a terminal, which the picker
// needs.
func Interactive(in, out *os.File) bool {
	return in != nil && out != nil && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

// Terminal is a terminal in raw mode with the cursor hidden. Close restores it.
type Terminal struct {
	in, out *os.File
	state   *term.State
	vt      func()
	drawn   int
}

// Open puts the terminal into raw mode for picking.
func Open(in, out *os.File) (*Terminal, error) {
	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil, err
	}
	t := &Terminal{in: in, out: out, state: state, vt: platform.EnableVT(out)}
	fmt.Fprint(out, "\x1b[?25l")
	return t, nil
}

// Close clears the picker, shows the cursor and restores the terminal.
func (t *Terminal) Close() {
	t.clear()
	fmt.Fprint(t.out, "\x1b[?25h")
	t.vt()
	term.Restore(int(t.in.Fd()), t.state)
}

// Pick runs a screen until it ends and returns its final state.
func (t *Terminal) Pick(s Screen) (Screen, Outcome, error) {
	buf := make([]byte, 64)
	for {
		t.draw(s)
		n, err := t.in.Read(buf)
		if err != nil {
			return s, Quit, err
		}
		for _, k := range Keys(buf[:n]) {
			if o := s.Handle(k); o != Pending {
				return s, o, nil
			}
		}
	}
}

func (t *Terminal) draw(s Screen) {
	width, _, err := term.GetSize(int(t.out.Fd()))
	if err != nil {
		width = 80
	}
	var b strings.Builder
	if t.drawn > 0 {
		fmt.Fprintf(&b, "\x1b[%dA", t.drawn)
	}
	lines := s.Lines(width - 1)
	for _, line := range lines {
		b.WriteString("\r\x1b[2K" + line + "\r\n")
	}
	b.WriteString("\x1b[J")
	fmt.Fprint(t.out, b.String())
	t.drawn = len(lines)
}

func (t *Terminal) clear() {
	if t.drawn > 0 {
		fmt.Fprintf(t.out, "\x1b[%dA\r\x1b[J", t.drawn)
		t.drawn = 0
	}
}
