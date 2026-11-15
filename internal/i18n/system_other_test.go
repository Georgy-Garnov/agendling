//go:build !windows

package i18n

import (
	"slices"
	"testing"
)

func TestLanguagesFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"LANG", map[string]string{"LANG": "fr_FR.UTF-8"}, []string{"fr_FR.UTF-8"}},
		{"LC_ALL wins", map[string]string{"LC_ALL": "de_DE.UTF-8", "LC_MESSAGES": "it_IT", "LANG": "fr_FR"}, []string{"de_DE.UTF-8"}},
		{"LC_MESSAGES before LANG", map[string]string{"LC_MESSAGES": "it_IT", "LANG": "fr_FR"}, []string{"it_IT"}},
		{"LANGUAGE list first", map[string]string{"LANGUAGE": "fr:de::C", "LANG": "en_US.UTF-8"}, []string{"fr", "de", "en_US.UTF-8"}},
		{"C locale ignores LANGUAGE", map[string]string{"LANGUAGE": "fr", "LANG": "C.UTF-8"}, nil},
		{"POSIX", map[string]string{"LC_ALL": "POSIX"}, nil},
		{"nothing set", map[string]string{"LANGUAGE": "fr"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := languagesFromEnv(func(k string) string { return tt.env[k] })
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
