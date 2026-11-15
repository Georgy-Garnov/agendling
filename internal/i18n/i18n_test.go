package i18n

import (
	"strings"
	"testing"
	"time"
)

// testCatalog is a made-up language with three plural forms, so the tests cover rules that do
// not fit English's one/other.
func testCatalog() *Catalog {
	return &Catalog{
		Tag:  "qps",
		Name: "Pseudo",
		PluralForm: func(n int) int {
			switch {
			case n == 1:
				return 0
			case n >= 2 && n <= 4:
				return 1
			}
			return 2
		},
		Messages: map[string]string{
			"Settings":               "Réglages",
			"starts in %s":           "commence dans %s",
			"Monday, 2 January 2006": "Monday 2 January 2006",
			"January 2006":           "January 2006",
			"2 Jan, Mon":             "2 Jan, Mon",
			"Blank":                  "",
		},
		Plurals: map[string][]string{
			"%d calendar": {"%d calendrier", "%d calendriers (few)", "%d calendriers (many)"},
		},
		Days:        [7]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
		ShortDays:   [7]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."},
		Months:      [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", ""},
		ShortMonths: [12]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
	}
}

// withCatalog makes c the current language for the rest of the test.
func withCatalog(t *testing.T, c *Catalog) {
	t.Helper()
	prev := current.Load()
	current.Store(c)
	t.Cleanup(func() { current.Store(prev) })
}

// registered adds c to the registry for the rest of the test.
func registered(t *testing.T, c *Catalog) {
	t.Helper()
	Register(c)
	t.Cleanup(func() {
		regMu.Lock()
		delete(registry, c.Tag)
		regMu.Unlock()
	})
}

func TestEnglishIsTheKey(t *testing.T) {
	if got := T("Settings"); got != "Settings" {
		t.Fatalf("T = %q", got)
	}
	if got := Tf("starts in %s", "5 min"); got != "starts in 5 min" {
		t.Fatalf("Tf = %q", got)
	}
	if got := Mark("Day"); got != "Day" {
		t.Fatalf("Mark = %q", got)
	}
}

func TestTranslateWithFallback(t *testing.T) {
	withCatalog(t, testCatalog())
	tests := []struct{ in, want string }{
		{"Settings", "Réglages"},
		{"Not translated yet", "Not translated yet"},
		{"Blank", "Blank"}, // an empty translation counts as missing
	}
	for _, tt := range tests {
		if got := T(tt.in); got != tt.want {
			t.Errorf("T(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := Tf("starts in %s", "5 min"); got != "commence dans 5 min" {
		t.Errorf("Tf = %q", got)
	}
}

func TestPlurals(t *testing.T) {
	for n, want := range map[int]string{0: "%d calendars", 1: "%d calendar", 2: "%d calendars"} {
		if got := TN(n, "%d calendar", "%d calendars"); got != want {
			t.Errorf("English TN(%d) = %q, want %q", n, got, want)
		}
	}
	withCatalog(t, testCatalog())
	for n, want := range map[int]string{1: "%d calendrier", 3: "%d calendriers (few)", 7: "%d calendriers (many)"} {
		if got := TN(n, "%d calendar", "%d calendars"); got != want {
			t.Errorf("TN(%d) = %q, want %q", n, got, want)
		}
	}
	if got := TN(2, "%d account", "%d accounts"); got != "%d accounts" {
		t.Errorf("untranslated plural = %q, want the English form", got)
	}
}

func TestPluralRuleOutOfRange(t *testing.T) {
	c := testCatalog()
	c.PluralForm = func(int) int { return 9 }
	withCatalog(t, c)
	if got := TN(2, "%d calendar", "%d calendars"); got != "%d calendars" {
		t.Fatalf("TN = %q, want the English fallback", got)
	}
}

func TestDateEnglishMatchesTimeFormat(t *testing.T) {
	d := time.Date(2026, 9, 28, 9, 5, 0, 0, time.UTC)
	for _, layout := range []string{"Monday, 2 January 2006", "Mon, 2 Jan 2006  15:04", "January 2006", "02.01 15:04", "Mon 2", "Montag Janvier Mon"} {
		if got, want := Date(d, layout), d.Format(layout); got != want {
			t.Errorf("Date(%q) = %q, want %q", layout, got, want)
		}
	}
}

func TestDateLocalized(t *testing.T) {
	withCatalog(t, testCatalog())
	mon := time.Date(2026, 9, 28, 9, 5, 0, 0, time.UTC) // a Monday
	tests := []struct {
		t      time.Time
		layout string
		want   string
	}{
		{mon, "Monday, 2 January 2006", "Lundi 28 septembre 2026"}, // translated layout, capitalized
		{mon, "January 2006", "Septembre 2026"},
		{mon, "2 Jan, Mon", "28 sept., lun."},           // names inside the text stay lowercase
		{mon, "Mon 2 Jan 15:04", "Lun. 28 sept. 09:05"}, // untranslated layout keeps its order
		{mon, "02.01 15:04", "28.09 09:05"},
		{mon, "Montag Janvier Mon", "Montag Janvier lun."}, // words starting like a token are text

		{time.Date(2026, 12, 3, 0, 0, 0, 0, time.UTC), "January", "December"}, // empty name → English
	}
	for _, tt := range tests {
		if got := Date(tt.t, tt.layout); got != tt.want {
			t.Errorf("Date(%q) = %q, want %q", tt.layout, got, tt.want)
		}
	}
}

func TestBaseTag(t *testing.T) {
	for in, want := range map[string]string{"fr-FR": "fr", "fr_CA.UTF-8": "fr", "FR": "fr", " de@euro ": "de", "": ""} {
		if got := baseTag(in); got != want {
			t.Errorf("baseTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUse(t *testing.T) {
	registered(t, testCatalog())
	t.Cleanup(func() { current.Store(English) })

	if got := Use("QPS-XA"); got != "qps" || Current() != "qps" || T("Settings") != "Réglages" {
		t.Fatalf("Use(QPS-XA) = %q, current %q", got, Current())
	}
	if got := Use("xx"); got != "en" || T("Settings") != "Settings" {
		t.Fatalf("unknown language: Use = %q, want en", got)
	}
	if got := lookup([]string{"de-DE", "qps_ZZ", "en-US"}); got.Tag != "qps" {
		t.Fatalf("lookup took %q, want the first available system language", got.Tag)
	}
	if got := lookup(nil); got != English {
		t.Fatalf("no system language: %q, want English", got.Tag)
	}
	if got := Use(""); got == "" { // follows the system; any registered language is fine
		t.Fatal("Use(\"\") returned no language")
	}
}

func TestLanguages(t *testing.T) {
	registered(t, testCatalog())
	langs := Languages()
	if len(langs) < 2 || langs[0] != (Language{"en", "English"}) {
		t.Fatalf("Languages() = %v, want English first", langs)
	}
	found := false
	for _, l := range langs {
		found = found || l == Language{"qps", "Pseudo"}
	}
	if !found {
		t.Fatalf("Languages() = %v, missing the registered language", langs)
	}
}

func TestRegisterRejectsDuplicatesAndIncompleteCatalogs(t *testing.T) {
	for name, c := range map[string]*Catalog{
		"duplicate": {Tag: "en", PluralForm: English.PluralForm},
		"uppercase": {Tag: "FR", PluralForm: English.PluralForm},
		"no tag":    {PluralForm: English.PluralForm},
		"no rule":   {Tag: "zz"},
		"nil":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil || !strings.HasPrefix(r.(string), "i18n:") {
					t.Fatalf("Register did not panic (%v)", r)
				}
			}()
			Register(c)
		})
	}
}
