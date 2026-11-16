//go:build !windows

package i18n

import (
	"os"
	"strings"
)

// SystemLanguages returns the user's preferred languages from the POSIX locale variables, most
// preferred first. LANGUAGE (a colon-separated list) only counts when a locale is set, as in
// GNU gettext; "C" and "POSIX" mean no preference.
func SystemLanguages() []string {
	return languagesFromEnv(os.Getenv)
}

func languagesFromEnv(getenv func(string) string) []string {
	locale := ""
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale = getenv(v); locale != "" {
			break
		}
	}
	if locale == "" || isCLocale(locale) {
		return nil
	}
	var res []string
	for _, l := range strings.Split(getenv("LANGUAGE"), ":") {
		if l != "" && !isCLocale(l) {
			res = append(res, l)
		}
	}
	return append(res, locale)
}

func isCLocale(s string) bool {
	base, _, _ := strings.Cut(s, ".")
	return base == "C" || base == "POSIX"
}
