package quota

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// ANSI styles shared by every quota view.
const (
	Reset  = "\x1b[0m"
	Dim    = "\x1b[2m"
	green  = "\x1b[1;32m"
	yellow = "\x1b[1;33m"
	red    = "\x1b[1;31m"
)

// Color is the style for a used fraction: green below 50%, yellow below 80%,
// red from there.
func Color(used float64) string {
	switch {
	case used < 0.5:
		return green
	case used < 0.8:
		return yellow
	default:
		return red
	}
}

// Bar is a bar of cells cells for a used fraction, in its color.
func Bar(used float64, cells int) string {
	filled := int(math.Round(math.Max(0, math.Min(1, used)) * float64(cells)))
	return Color(used) + "[" + strings.Repeat("▰", filled) + strings.Repeat("▱", cells-filled) + "]" + Reset
}

// Percent is a used fraction as a whole percentage, in its color.
func Percent(used float64) string {
	return Color(used) + fmt.Sprintf("%d%%", int(math.Round(used*100))) + Reset
}

// ResetLabel is when a window resets, in local time: the clock time when it
// is within a day, the date and time otherwise.
func ResetLabel(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	if t.Sub(now) < 24*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("01-02 15:04")
}

// Age says how long ago t was, coarsely.
func Age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
