package scheduler

import (
	"os"
	"testing"

	"github.com/Georgy-Garnov/agendling/internal/secret"
)

func TestMain(m *testing.M) {
	dir, _ := os.MkdirTemp("", "gocal-secret")
	secret.UseKeyFileIn(dir) // keep tests away from the user's keyring and config
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
