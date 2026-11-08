package caldav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
)

// propfind sends a raw PROPFIND and decodes the 207 multistatus into v.
// go-webdav keeps its PROPFIND helpers internal and drops the host of returned hrefs.
func propfind(ctx context.Context, hc webdav.HTTPClient, u *url.URL, depth, body string, v any) error {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", u.String(), strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("Depth", depth)
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return fmt.Errorf("PROPFIND %s: %s", u.Path, resp.Status)
	}
	if err := xml.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("PROPFIND %s: %w", u.Path, err)
	}
	return nil
}

// findCurrentUserPrincipal preserves the configured URL's trailing slash.
// go-webdav's path.Join removes it, which some servers route to a non-DAV handler.
func findCurrentUserPrincipal(ctx context.Context, hc webdav.HTTPClient, endpoint *url.URL) (string, error) {
	const body = `<d:propfind xmlns:d="DAV:"><d:prop><d:current-user-principal/></d:prop></d:propfind>`
	var ms struct {
		Responses []struct {
			Propstat []struct {
				Prop struct {
					Principal struct {
						Href string `xml:"DAV: href"`
					} `xml:"DAV: current-user-principal"`
				} `xml:"DAV: prop"`
			} `xml:"DAV: propstat"`
		} `xml:"DAV: response"`
	}
	if err := propfind(ctx, hc, endpoint, "0", body, &ms); err != nil {
		return "", err
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if href := strings.TrimSpace(ps.Prop.Principal.Href); href != "" {
				ref, err := url.Parse(href)
				if err != nil {
					return "", fmt.Errorf("invalid principal URL: %w", err)
				}
				return endpoint.ResolveReference(ref).Path, nil
			}
		}
	}
	return "", errors.New("server reports no current-user-principal")
}

const homeSetPropfind = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><c:calendar-home-set/></d:prop></d:propfind>`

type homeSetMultistatus struct {
	Responses []struct {
		Propstat []struct {
			Prop struct {
				HomeSet struct {
					Href string `xml:"DAV: href"`
				} `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

// findHomeSet returns the calendar home set of a principal as an absolute URL. Some servers
// (iCloud) put it on another host than the one the account was set up with, and every
// collection request has to go there.
func findHomeSet(ctx context.Context, hc webdav.HTTPClient, endpoint *url.URL, principal string) (*url.URL, error) {
	var ms homeSetMultistatus
	if err := propfind(ctx, hc, endpoint.ResolveReference(&url.URL{Path: principal}), "0", homeSetPropfind, &ms); err != nil {
		return nil, err
	}
	for _, r := range ms.Responses {
		for _, ps := range r.Propstat {
			if href := strings.TrimSpace(ps.Prop.HomeSet.Href); href != "" {
				ref, err := url.Parse(href)
				if err != nil {
					return nil, fmt.Errorf("calendar-home-set %q: %w", href, err)
				}
				return endpoint.ResolveReference(ref), nil
			}
		}
	}
	return nil, errors.New("server reports no calendar-home-set")
}

// sameService reports whether credentials for from may be sent to to: the same host name over
// the same scheme, or an HTTPS host under the same parent domain (caldav.icloud.com ->
// p34-caldav.icloud.com). Bare two-label parents like "com" never qualify.
func sameService(from, to *url.URL) bool {
	if from.Hostname() == to.Hostname() {
		return from.Scheme == to.Scheme || to.Scheme == "https"
	}
	if to.Scheme != "https" {
		return false
	}
	fh, th := strings.ToLower(from.Hostname()), strings.ToLower(to.Hostname())
	if strings.HasSuffix(th, "."+fh) {
		return true
	}
	_, parent, ok := strings.Cut(fh, ".")
	return ok && strings.Contains(parent, ".") && strings.HasSuffix(th, "."+parent)
}

// rootURL is the scheme and host of u, the base all server paths are resolved against.
func rootURL(u *url.URL) string {
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}).String()
}

// calendarObject is one calendar resource returned by a query.
type calendarObject struct {
	Path string
	ETag string
	Data *ical.Calendar
}

type queryMultistatus struct {
	Responses []struct {
		Href     string `xml:"DAV: href"`
		Propstat []struct {
			Prop struct {
				ETag string `xml:"DAV: getetag"`
				Data string `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

// queryEvents runs a calendar-query REPORT for VEVENTs overlapping [from, to) and returns the
// full objects. It asks for plain <calendar-data/>: iCloud answers go-webdav's
// <comp name="VCALENDAR"><allprop/><allcomp/></comp> with calendars stripped of their events.
func queryEvents(ctx context.Context, hc webdav.HTTPClient, endpoint *url.URL, coll string, from, to time.Time) ([]calendarObject, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>` +
		`<C:calendar-query xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">` +
		`<D:prop><D:getetag/><C:calendar-data/></D:prop>` +
		`<C:filter><C:comp-filter name="VCALENDAR"><C:comp-filter name="VEVENT">` +
		`<C:time-range start="` + from.UTC().Format(icalUTC) + `" end="` + to.UTC().Format(icalUTC) + `"/>` +
		`</C:comp-filter></C:comp-filter></C:filter></C:calendar-query>`
	u := endpoint.ResolveReference(&url.URL{Path: coll})
	req, err := http.NewRequestWithContext(ctx, "REPORT", u.String(), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("Depth", "1")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("REPORT %s: %s %s", coll, resp.Status, strings.TrimSpace(string(msg)))
	}
	var ms queryMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("REPORT %s: %w", coll, err)
	}

	var objs []calendarObject
	for _, r := range ms.Responses {
		var etag, data string
		for _, ps := range r.Propstat {
			if ps.Prop.ETag != "" {
				etag = ps.Prop.ETag
			}
			if ps.Prop.Data != "" {
				data = ps.Prop.Data
			}
		}
		if strings.TrimSpace(data) == "" {
			continue // the collection itself, or an object the server sent without data
		}
		hu, err := url.Parse(r.Href)
		if err != nil {
			log.Printf("caldav: skip object with bad href %q: %v", r.Href, err)
			continue
		}
		cal, err := ical.NewDecoder(strings.NewReader(data)).Decode()
		if err != nil {
			log.Printf("caldav: skip unparsable object %s: %v", hu.Path, err)
			continue
		}
		objs = append(objs, calendarObject{Path: hu.Path, ETag: normalizeETag(etag), Data: cal})
	}
	return objs, nil
}

const icalUTC = "20060102T150405Z"
