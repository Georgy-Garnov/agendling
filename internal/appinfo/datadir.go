package appinfo

import (
	"os"
	"path/filepath"
)

// DataDir returns the per-user data directory (%AppData%\Agendling on Windows,
// ~/.config/Agendling on Linux), creating it and migrating a legacy directory if present.
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, Name)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		legacy := filepath.Join(base, LegacyDirName)
		if _, err := os.Stat(legacy); err == nil {
			if err := os.Rename(legacy, dir); err != nil {
				// Still in use (e.g. an old build is running): keep using it for now.
				return legacy, nil
			}
		}
	}
	return dir, os.MkdirAll(dir, 0o700)
}
