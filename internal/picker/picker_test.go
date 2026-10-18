package picker

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestKeys(t *testing.T) {
	for in, want := range map[string][]Key{
		"\x1b[A":     {KeyUp},
		"\x1bOB":     {KeyDown}, // application cursor mode
		"\x1b[D":     {KeyBack},
		"\x1b":       {KeyBack},
		"jjk":        {KeyDown, KeyDown, KeyUp},
		"\r":         {KeyEnter},
		"q":          {KeyQuit},
		"\x03":       {KeyQuit},
		"3":          {Key1 + 2},
		"x":          nil,
		"\x1b[B\x1b": {KeyDown, KeyBack},
	} {
		if got := Keys([]byte(in)); !reflect.DeepEqual(got, want) {
			t.Errorf("Keys(%q) = %v, want %v", in, got, want)
		}
	}
}

func screen(n int) Screen {
	s := Screen{Title: "t"}
	for i := 0; i < n; i++ {
		s.Items = append(s.Items, Item{Label: string(rune('a' + i))})
	}
	return s
}

func TestHandleMovesWithinBounds(t *testing.T) {
	s := screen(3)
	for _, k := range []Key{KeyUp, KeyDown, KeyDown, KeyDown} {
		if o := s.Handle(k); o != Pending {
			t.Fatalf("%v ended the screen: %v", k, o)
		}
	}
	if s.Cursor != 2 {
		t.Fatalf("cursor = %d, want clamped at 2", s.Cursor)
	}
	if s.Handle(KeyEnter) != Chosen || s.Cursor != 2 {
		t.Fatal("enter did not choose the cursor item")
	}
}

func TestHandleDigitsBackAndQuit(t *testing.T) {
	s := screen(3)
	if s.Handle(Key1+1) != Chosen || s.Cursor != 1 {
		t.Fatalf("digit 2 → cursor %d", s.Cursor)
	}
	if s.Handle(Key1+5) != Pending {
		t.Fatal("digit beyond the list chose something")
	}
	if s.Handle(KeyBack) != Pending {
		t.Fatal("back without CanGoBack ended the screen")
	}
	s.CanGoBack = true
	if s.Handle(KeyBack) != Back || s.Handle(KeyQuit) != Quit {
		t.Fatal("back/quit")
	}
	empty := screen(0)
	if empty.Handle(KeyEnter) != Pending {
		t.Fatal("enter on an empty list chose something")
	}
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestLinesMarkCursorAndTag(t *testing.T) {
	s := Screen{Title: "Pick", Items: []Item{
		{Label: "claude-pool", Detail: "astergate", Tag: "last"},
		{Label: "x", Detail: "y"},
	}, Cursor: 1}
	lines := s.Lines(0)
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = ansi.ReplaceAllString(l, "")
	}
	if !strings.HasPrefix(plain[0], "Pick  ") || plain[1] != "  claude-pool (last)  astergate" || plain[2] != "❯ x"+strings.Repeat(" ", 19)+"y" {
		t.Fatalf("lines = %q", plain)
	}
	// A narrow terminal cuts the detail, not the tag.
	if narrow := ansi.ReplaceAllString(s.Lines(20)[1], ""); narrow != "  claude-pool (last)" {
		t.Fatalf("narrow line = %q", narrow)
	}
}

func TestFitCutsVisibleColumnsOnly(t *testing.T) {
	got := fit(cyan+"abcdef"+reset, 3)
	if plain := ansi.ReplaceAllString(got, ""); plain != "abc" {
		t.Fatalf("fit = %q", plain)
	}
	if !strings.HasSuffix(got, reset) {
		t.Fatal("cut line does not close its style")
	}
	if fit("ab", 5) != "ab" || utf8.RuneCountInString(ansi.ReplaceAllString(fit("❯ 中文字", 3), "")) != 3 {
		t.Fatal("fit miscounts")
	}
}
