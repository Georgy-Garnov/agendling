//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// detectLocalTZID reads $TZ, /etc/timezone or the /etc/localtime symlink target.
func detectLocalTZID() string {
	candidates := []string{strings.TrimPrefix(os.Getenv("TZ"), ":")}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		candidates = append(candidates, strings.TrimSpace(string(b)))
	}
	if target, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			candidates = append(candidates, target[i+len("zoneinfo/"):])
		}
	}
	for _, c := range candidates {
		if c == "" || c == "Local" {
			continue
		}
		if _, err := time.LoadLocation(c); err == nil {
			return c
		}
	}
	return ""
}
