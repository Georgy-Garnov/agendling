package core

import (
	"errors"
	"strings"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/i18n"
)

// DateFieldText is what a date entry shows: the weekday, then the date, both in the current
// language ("Mon 02.01.2006" in English).
func DateFieldText(t time.Time) string { return i18n.Date(t, "Mon 02.01.2006") }

// ParseDateField reads a date entry. The weekday before the date is skipped, so the text of
// DateFieldText reads back in any language. The date is read with the current language's layout
// (dots in English), then as DD.MM.YYYY, D.M.YYYY or YYYY-MM-DD.
func ParseDateField(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	date := s
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		date = s[i+1:]
	}
	layout := i18n.T("Mon 02.01.2006")
	if i := strings.LastIndexByte(layout, ' '); i >= 0 {
		layout = layout[i+1:]
	}
	for _, l := range []string{layout, "02.01.2006", "2.1.2006", "2006-01-02"} {
		if t, err := time.ParseInLocation(l, date, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New(i18n.Tf("invalid date %q (use DD.MM.YYYY)", s))
}
