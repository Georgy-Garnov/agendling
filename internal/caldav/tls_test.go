package caldav

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

func TestCertificateVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	for _, skip := range []bool{false, true, false} {
		client := newHTTPClient(&model.CalendarSource{SkipTLSVerify: skip})
		req, err := http.NewRequest(http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if skip && err != nil {
			t.Fatalf("verification disabled: %v", err)
		}
		if !skip && err == nil {
			t.Fatal("untrusted certificate accepted with verification enabled")
		}
	}
}
