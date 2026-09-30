package caldav

import (
	"bytes"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/emersion/go-webdav"
)

// etagFixClient works around servers that send weak (W/"...") or unquoted
// ETags. go-webdav parses ETags with strconv.Unquote and fails the whole request otherwise,
// so every ETag in headers and <getetag> elements is rewritten into a strict quoted string.
type etagFixClient struct {
	c webdav.HTTPClient
}

var getetagRe = regexp.MustCompile(`(?s)(<(?:[A-Za-z0-9_-]+:)?getetag(?:\s[^>]*)?>)(.*?)(</(?:[A-Za-z0-9_-]+:)?getetag>)`)

func (e etagFixClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := e.c.Do(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if v := resp.Header.Get("ETag"); v != "" {
		resp.Header.Set("ETag", strconv.Quote(normalizeETag(v)))
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "xml") || resp.Body == nil {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	body = getetagRe.ReplaceAllFunc(body, func(m []byte) []byte {
		parts := getetagRe.FindSubmatch(m)
		value := normalizeETag(html.UnescapeString(string(parts[2])))
		quoted := html.EscapeString(strconv.Quote(value))
		return append(append(append([]byte{}, parts[1]...), quoted...), parts[3]...)
	})
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Del("Content-Length")
	return resp, nil
}

// normalizeETag strips the weak prefix and surrounding quotes, returning the opaque value.
func normalizeETag(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "W/")
	if s, err := strconv.Unquote(v); err == nil {
		return s
	}
	return strings.Trim(v, `"`)
}
