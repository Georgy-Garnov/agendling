//go:build windows

package ui

import (
	"strings"

	"github.com/Georgy-Garnov/agendling/internal/core"
)

// LinkMarkup renders plain text as SysLink markup in which every URL is a clickable
// link. SysLink has no escaping, so only URLs become tags; "<" elsewhere is neutralized.
func LinkMarkup(text string, maxLinkLen int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var b strings.Builder
	last := 0
	for _, loc := range core.URLRegexp.FindAllStringIndex(text, -1) {
		b.WriteString(sanitizeLinkText(text[last:loc[0]]))
		u := text[loc[0]:loc[1]]
		href := u
		if !strings.Contains(href, "://") {
			href = "https://" + href
		}
		b.WriteString(`<a href="` + href + `">` + sanitizeLinkText(core.Shorten(u, maxLinkLen)) + `</a>`)
		last = loc[1]
	}
	b.WriteString(sanitizeLinkText(text[last:]))
	return strings.ReplaceAll(b.String(), "\n", "\r\n")
}

func sanitizeLinkText(s string) string {
	// A literal "<a" or "</a" in plain text would be parsed as markup.
	return strings.ReplaceAll(s, "<", "‹")
}
