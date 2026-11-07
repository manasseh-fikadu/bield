package cli

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// A style is a color for a word in a table. It does nothing unless stdout is a
// terminal that takes color.
type style int

const (
	plain style = iota
	good
	warn
	bad
	dim
	bold
)

var ansi = map[style]string{good: "32", warn: "33", bad: "31", dim: "90", bold: "1"}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// A View writes a result for a person to read.
type View struct {
	w     io.Writer
	color bool
	now   time.Time
	err   error
}

// Paint returns text in a style, or as it is if there's no color.
func (v *View) Paint(s style, text string) string {
	if !v.color || s == plain || text == "" {
		return text
	}
	return "\x1b[" + ansi[s] + "m" + text + "\x1b[0m"
}

// Printf writes formatted text.
func (v *View) Printf(format string, args ...any) {
	if v.err == nil {
		_, v.err = fmt.Fprintf(v.w, format, args...)
	}
}

// Line writes a line.
func (v *View) Line(s string) { v.Printf("%s\n", s) }

// Field writes a "name: value" line for a detail view, with the names lined up.
func (v *View) Field(name, value string) {
	if value == "" {
		return
	}
	v.Printf("%s  %s\n", v.Paint(dim, fmt.Sprintf("%-14s", name)), value)
}

// Table starts a table with columns.
func (v *View) Table(head ...string) *Table { return &Table{v: v, head: head} }

// A Table is rows of cells, lined up in columns.
type Table struct {
	v    *View
	head []string
	rows [][]string
}

// Row adds a row of cells.
func (t *Table) Row(cells ...string) { t.rows = append(t.rows, cells) }

// Len returns the number of rows.
func (t *Table) Len() int { return len(t.rows) }

// Write writes the table. The last column isn't padded, so lines end where
// their text does.
func (t *Table) Write() {
	widths := make([]int, len(t.head))
	for i, h := range t.head {
		widths[i] = visible(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) {
				widths[i] = max(widths[i], visible(c))
			}
		}
	}
	line := func(cells []string, head bool) {
		var b strings.Builder
		for i, c := range cells {
			if i == len(cells)-1 {
				b.WriteString(c)
				break
			}
			b.WriteString(c)
			b.WriteString(strings.Repeat(" ", widths[i]-visible(c)+2))
		}
		s := strings.TrimRight(b.String(), " ")
		if head {
			s = t.v.Paint(dim, s)
		}
		t.v.Line(s)
	}
	line(t.head, true)
	for _, r := range t.rows {
		line(r, false)
	}
}

// visible is how many characters of text show: not the color codes.
func visible(s string) int { return utf8.RuneCountInString(ansiSeq.ReplaceAllString(s, "")) }

// ago says how long before now at is, briefly: "5m", "3h", "2d". A time in
// the future or a zero time gives "-".
func (v *View) ago(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return age(v.now.Sub(at))
}

func age(d time.Duration) string {
	switch {
	case d < 0:
		return "-"
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

// since says how long ago at was in words: "just now" or "5m ago".
func (v *View) since(at time.Time) string {
	if a := v.ago(at); a != "now" && a != "-" {
		return a + " ago"
	}
	return "just now"
}

// until says how long from now at is: "5m", "3h", "2d", or "-" if it's past.
func (v *View) until(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	return age(at.Sub(v.now))
}

// bytesLabel writes a size with a binary unit: "1.5 GiB".
func bytesLabel(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	f, i := float64(n), 0
	for f >= unit && i < 5 {
		f /= unit
		i++
	}
	return fmt.Sprintf("%.1f %ciB", f, "KMGTPE"[i-1])
}

// percent writes a share of something as "42%", or "-" if there's no value.
func percent(f *float64) string {
	if f == nil || math.IsNaN(*f) {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", *f)
}

// share writes used as a percent of total, or "-" if there's no total.
func share(used, total uint64) string {
	if total == 0 {
		return "-"
	}
	f := float64(used) / float64(total) * 100
	return percent(&f)
}

// oneLine returns s with its line breaks turned to spaces and cut to n
// characters, so a long message fits in a table.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// clock writes a time as the person sees it where they run bield.
func clock(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func formatInt(n int64) string { return strconv.FormatInt(n, 10) }
