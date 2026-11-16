// Package i18n translates Agendling's own user-facing text.
//
// The English text is the key: code calls T("Settings") and gets "Settings" back unless the
// current language has a translation for it. English needs no catalog and is the fallback for
// any missing message, so an incomplete translation still shows every string.
//
// Strings kept in tables (view names, repeat presets, settings pages…) are marked with Mark and
// translated with T where they are shown, so the catalog test can find them.
//
// The package has no dependencies inside Agendling, so every package can use it, including
// those below internal/core.
package i18n

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Catalog holds one language's translations. It must not be changed once registered: it is read
// from any goroutine without locking.
type Catalog struct {
	Tag  string // lowercase ISO 639-1 code, e.g. "fr"
	Name string // the language's own name, shown in Settings, e.g. "Français"

	// PluralForm maps a count to an index into the forms of a Plurals entry.
	PluralForm func(n int) int

	// Messages maps an English text, or an English date layout used with Date, to its
	// translation.
	Messages map[string]string
	// Plurals maps the English singular passed to TN to the translated forms, in PluralForm
	// order.
	Plurals map[string][]string

	// Day and month names used by Date: Days and ShortDays start on Sunday like time.Weekday,
	// Months and ShortMonths on January. Names are written as they appear inside a sentence;
	// Date capitalizes one that starts the result.
	Days, ShortDays     [7]string
	Months, ShortMonths [12]string
}

// English is the built-in language: its messages are the keys themselves.
var English = &Catalog{
	Tag:        "en",
	Name:       "English",
	PluralForm: func(n int) int { return boolIndex(n != 1) },
	Days:       [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	ShortDays:  [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	Months: [12]string{"January", "February", "March", "April", "May", "June", "July", "August",
		"September", "October", "November", "December"},
	ShortMonths: [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
}

var (
	regMu    sync.RWMutex
	registry = map[string]*Catalog{English.Tag: English}

	current atomic.Pointer[Catalog]
)

func init() { current.Store(English) }

// Register adds a language. Catalogs register themselves from an init function; registering
// the same tag twice is a programming error.
func Register(c *Catalog) {
	if c == nil || c.Tag == "" || c.PluralForm == nil {
		panic("i18n: catalog needs a tag and a plural rule")
	}
	if c.Tag != strings.ToLower(c.Tag) {
		panic("i18n: language tag must be lowercase: " + c.Tag)
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[c.Tag]; dup {
		panic("i18n: language registered twice: " + c.Tag)
	}
	registry[c.Tag] = c
}

// Language is an entry of the language picker.
type Language struct {
	Tag  string
	Name string
}

// Languages lists the available languages: English first, then the others by tag.
func Languages() []Language {
	regMu.RLock()
	defer regMu.RUnlock()
	res := make([]Language, 0, len(registry))
	for _, c := range registry {
		if c != English {
			res = append(res, Language{c.Tag, c.Name})
		}
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Tag < res[j].Tag })
	return append([]Language{{English.Tag, English.Name}}, res...)
}

// Use selects the language for the rest of the run and returns its tag. An empty preference
// follows the system's preferred languages; a preference or system language without a catalog
// falls back to English. The language is chosen once at startup: the UI is not rebuilt when it
// changes.
func Use(preference string) string {
	candidates := SystemLanguages()
	if preference != "" {
		candidates = []string{preference}
	}
	c := lookup(candidates)
	current.Store(c)
	return c.Tag
}

// Current returns the tag of the language in use.
func Current() string { return current.Load().Tag }

func lookup(candidates []string) *Catalog {
	regMu.RLock()
	defer regMu.RUnlock()
	for _, cand := range candidates {
		if c, ok := registry[baseTag(cand)]; ok {
			return c
		}
	}
	return English
}

// baseTag reduces "fr-FR", "fr_CA.UTF-8" or "FR" to "fr".
func baseTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_.@"); i >= 0 {
		s = s[:i]
	}
	return s
}

// T translates an English text.
func T(s string) string {
	return current.Load().message(s)
}

// Tf translates an English format string and formats it with args.
func Tf(format string, args ...any) string {
	return fmt.Sprintf(current.Load().message(format), args...)
}

// TN picks the translated singular or plural form for n, e.g. TN(n, "%d calendar",
// "%d calendars"). It returns the form unformatted: the caller passes it to fmt with n.
func TN(n int, one, other string) string {
	c := current.Load()
	if forms, ok := c.Plurals[one]; ok {
		if i := c.PluralForm(n); i >= 0 && i < len(forms) && forms[i] != "" {
			return forms[i]
		}
	}
	if n == 1 {
		return one
	}
	return other
}

// Mark returns s unchanged. It marks an English text stored in a table, to be translated with
// T where it is shown, so the catalog test knows it is a message.
func Mark(s string) string { return s }

func (c *Catalog) message(s string) string {
	if t, ok := c.Messages[s]; ok && t != "" {
		return t
	}
	return s
}

func boolIndex(b bool) int {
	if b {
		return 1
	}
	return 0
}
