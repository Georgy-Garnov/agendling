package caldav

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

func tlsCalDAV(t *testing.T) (*httptest.Server, *memBackend) {
	backend := &memBackend{objs: map[string]caldav.CalendarObject{}}
	srv := httptest.NewTLSServer(&caldav.Handler{Backend: backend})
	t.Cleanup(srv.Close)
	return srv, backend
}

// A NAS with a self-signed certificate: the account fails until the user trusts that very
// certificate, then works for Test connection and sync alike.
func TestTrustedCertificate(t *testing.T) {
	srv, backend := tlsCalDAV(t)
	ctx := context.Background()
	src := &model.CalendarSource{Name: "NAS", Type: model.SourceCalDAV, URL: srv.URL + "/", Username: "u", Enabled: true}

	_, err := TestConnection(ctx, src)
	cert := UntrustedCertificate(err)
	if cert == nil {
		t.Fatalf("untrusted certificate not reported: %v", err)
	}
	if CertFingerprint(cert) != CertFingerprint(srv.Certificate()) {
		t.Fatal("reported a different certificate than the server's")
	}
	info := DescribeCertificate(cert)
	if len(info.Fingerprint) != 95 || strings.Count(info.Fingerprint, ":") != 31 || info.NotAfter.IsZero() {
		t.Fatalf("certificate description: %+v", info)
	}

	src.TrustedCert = CertFingerprint(cert)
	cals, err := TestConnection(ctx, src)
	if err != nil || len(cals) != 1 {
		t.Fatalf("trusted certificate: %v, %d calendars", err, len(cals))
	}

	start := time.Now().Add(24 * time.Hour).Truncate(time.Hour)
	backend.PutCalendarObject(ctx, calPath+"r.ics", ics.FromEvents([]*model.Event{{UID: "r", Title: "On the NAS",
		StartTime: start, EndTime: start.Add(time.Hour)}}), nil)
	store, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(src); err != nil {
		t.Fatal(err)
	}
	if again, err := store.Source(src.ID); err != nil || again.TrustedCert != src.TrustedCert {
		t.Fatalf("trusted certificate not stored: %v", err)
	}
	if _, err := SyncSource(ctx, store, src); err != nil {
		t.Fatalf("sync with the trusted certificate: %v", err)
	}
	if evs, _ := store.EventsInRange(start.Add(-time.Hour), start.Add(2*time.Hour)); len(evs) != 1 {
		t.Fatalf("synced %d events", len(evs))
	}
}

// Trusting one certificate is not trusting any: a different one (a replaced or spoofed server)
// is reported again, and the sync status says what to do.
func TestTrustedCertificateChanged(t *testing.T) {
	srv, _ := tlsCalDAV(t)
	previous := strings.Repeat("CD", 32) // the certificate trusted before
	src := &model.CalendarSource{Type: model.SourceCalDAV, URL: srv.URL + "/", TrustedCert: previous}
	_, err := TestConnection(context.Background(), src)
	cert := UntrustedCertificate(err)
	if cert == nil || CertFingerprint(cert) != CertFingerprint(srv.Certificate()) {
		t.Fatalf("changed certificate not reported: %v", err)
	}
	if msg := syncError(err).Error(); !strings.Contains(msg, "Test connection") {
		t.Fatalf("sync status: %q", msg)
	}
}

// A certificate the system trusts keeps working when the account also trusts another one (e.g.
// the NAS moved from a self-signed certificate to a proper one).
func TestSystemTrustStillApplies(t *testing.T) {
	srv, _ := tlsCalDAV(t)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	verifyRoots = pool
	defer func() { verifyRoots = nil }()
	src := &model.CalendarSource{Type: model.SourceCalDAV, URL: srv.URL + "/", TrustedCert: strings.Repeat("AB", 32)}
	if _, err := TestConnection(context.Background(), src); err != nil {
		t.Fatalf("system-trusted certificate rejected: %v", err)
	}
	if UntrustedCertificate(http.ErrHandlerTimeout) != nil {
		t.Fatal("an unrelated error taken for a certificate problem")
	}
}
