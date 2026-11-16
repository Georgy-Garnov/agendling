package core

import (
	"testing"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/i18n"
)

func TestParseDateField(t *testing.T) {
	t.Cleanup(func() { i18n.Use("en") })
	want := time.Date(2026, 9, 28, 0, 0, 0, 0, time.Local)
	for _, s := range []string{"Mon 28.09.2026", " lun. 28.09.2026 ", "28.09.2026", "28.9.2026", "2026-09-28"} {
		if got, err := ParseDateField(s); err != nil || !got.Equal(want) {
			t.Errorf("ParseDateField(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"Mon 31.02.2026", "09/28/2026", ""} {
		if _, err := ParseDateField(s); err == nil {
			t.Errorf("ParseDateField(%q) accepted", s)
		}
	}
	// What the field shows reads back in every language.
	for _, l := range i18n.Languages() {
		i18n.Use(l.Tag)
		if got, err := ParseDateField(DateFieldText(want)); err != nil || !got.Equal(want) {
			t.Errorf("%s: %q does not read back: %v, %v", l.Tag, DateFieldText(want), got, err)
		}
	}
}
