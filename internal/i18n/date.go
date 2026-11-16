package i18n

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Date formats t like t.Format(layout), in the current language: the layout is itself a message
// (so a language can reorder it, e.g. "Monday, 2 January 2006" → "Monday 2 January 2006"), and
// the day and month names come from the catalog.
func Date(t time.Time, layout string) string {
	c := current.Load()
	layout = c.message(layout)
	if c == English {
		return t.Format(layout)
	}
	var b strings.Builder
	start := 0
	for i := 0; i < len(layout); {
		name, n := c.nameAt(layout[i:], t)
		if n == 0 {
			i++
			continue
		}
		b.WriteString(t.Format(layout[start:i]))
		if b.Len() == 0 {
			name = capitalize(name)
		}
		b.WriteString(name)
		i += n
		start = i
	}
	b.WriteString(t.Format(layout[start:]))
	return b.String()
}

// nameAt returns the localized name for a day or month token at the start of s and the token's
// length, or 0 when s does not start with one. It follows time.Format's rules: longer tokens
// first, and "Jan" or "Mon" followed by a lowercase letter is text ("Janvier", "Montag").
func (c *Catalog) nameAt(s string, t time.Time) (string, int) {
	switch {
	case strings.HasPrefix(s, "January"):
		return orEnglish(c.Months[t.Month()-1], English.Months[t.Month()-1]), len("January")
	case strings.HasPrefix(s, "Jan") && !startsWithLower(s[3:]):
		return orEnglish(c.ShortMonths[t.Month()-1], English.ShortMonths[t.Month()-1]), len("Jan")
	case strings.HasPrefix(s, "Monday"):
		return orEnglish(c.Days[t.Weekday()], English.Days[t.Weekday()]), len("Monday")
	case strings.HasPrefix(s, "Mon") && !startsWithLower(s[3:]):
		return orEnglish(c.ShortDays[t.Weekday()], English.ShortDays[t.Weekday()]), len("Mon")
	}
	return "", 0
}

// startsWithLower matches time.Format's test, which only looks at ASCII letters.
func startsWithLower(s string) bool {
	return s != "" && 'a' <= s[0] && s[0] <= 'z'
}

func orEnglish(s, en string) string {
	if s == "" {
		return en
	}
	return s
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
