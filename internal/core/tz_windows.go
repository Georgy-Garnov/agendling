//go:build windows

package core

import (
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/Georgy-Garnov/agendling/internal/ics"
)

func detectLocalTZID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\TimeZoneInformation`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	key, _, err := k.GetStringValue("TimeZoneKeyName")
	if err != nil {
		return ""
	}
	iana, ok := ics.WindowsZoneToIANA(strings.TrimRight(key, "\x00"))
	if !ok {
		return ""
	}
	if _, err := time.LoadLocation(iana); err != nil {
		return ""
	}
	return iana
}
