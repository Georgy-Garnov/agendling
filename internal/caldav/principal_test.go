package caldav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPrincipalPreservesTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/caldav/" {
			http.Error(w, "not a DAV endpoint", http.StatusMethodNotAllowed)
			return
		}
		if r.Method != "PROPFIND" || r.Header.Get("Depth") != "0" {
			t.Errorf("unexpected request: %s, depth %s", r.Method, r.Header.Get("Depth"))
		}
		w.WriteHeader(http.StatusMultiStatus)
		io.WriteString(w, `<multistatus xmlns="DAV:"><response><propstat><prop><current-user-principal><href>/caldav.php/frank/</href></current-user-principal></prop><status>HTTP/1.1 200 OK</status></propstat></response></multistatus>`)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL + "/caldav/")
	principal, err := findCurrentUserPrincipal(context.Background(), server.Client(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if principal != "/caldav.php/frank/" {
		t.Fatalf("principal = %q", principal)
	}
}
