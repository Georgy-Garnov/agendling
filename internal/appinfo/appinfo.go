// Package appinfo holds the product identity used across the app.
package appinfo

import _ "embed"

const (
	// Name is the product name shown to users.
	Name = "Agendling"
	// ID is the lowercase identifier used for files, directories and system registrations.
	ID = "agendling"
	// LegacyDirName is the data directory used by pre-release builds; it is migrated on start.
	LegacyDirName = "GoCalendar"
	// Homepage is the project page.
	Homepage = "https://github.com/Georgy-Garnov/agendling"
)

// Version is set at build time: -ldflags "-X github.com/Georgy-Garnov/agendling/internal/appinfo.Version=v1.2.3".
var Version = "dev"

// LogoPNG is the app logo (256×256), shown in the About section.
//
//go:embed logo.png
var LogoPNG []byte
