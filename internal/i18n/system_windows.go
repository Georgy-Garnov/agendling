//go:build windows

package i18n

import (
	"log"

	"golang.org/x/sys/windows"
)

// SystemLanguages returns the user's preferred UI languages, most preferred first, such as
// ["fr-FR", "en-US"].
func SystemLanguages() []string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		log.Printf("i18n: cannot read the Windows display language: %v", err)
		return nil
	}
	return langs
}
