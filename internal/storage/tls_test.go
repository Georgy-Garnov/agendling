package storage

import (
	"path/filepath"
	"testing"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

func TestSkipTLSVerifyPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tls.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	src := &model.CalendarSource{Name: "LAN", Type: model.SourceCalDAV, SkipTLSVerify: true}
	if err := st.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.Source(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SkipTLSVerify {
		t.Fatal("TLS setting was not persisted")
	}
	got.SkipTLSVerify = false
	if err := st.SaveSource(got); err != nil {
		t.Fatal(err)
	}
	got, err = st.Source(src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SkipTLSVerify {
		t.Fatal("TLS verification could not be re-enabled")
	}
}
