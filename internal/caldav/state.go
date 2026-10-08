package caldav

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emersion/go-webdav"

	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// fullRefreshEvery bounds how long discovery results and change tags are trusted: a full pull
// also slides the query window and drops collections that disappeared from the server.
const fullRefreshEvery = 24 * time.Hour

// syncState is cached per source between syncs so an unchanged account costs a single PROPFIND.
type syncState struct {
	Home         string            `json:"home,omitempty"` // calendar home set ("" when the URL is the collection)
	Collections  []string          `json:"collections,omitempty"`
	CTags        map[string]string `json:"ctags,omitempty"` // collection path -> getctag / sync-token of the last pull
	FullAt       time.Time         `json:"full_at,omitzero"`
	BlockedUntil time.Time         `json:"blocked_until,omitzero"` // server rate limit in effect
}

func loadState(store *storage.Store, id int64) syncState {
	var st syncState
	if raw, err := store.SyncState(id); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &st) // a corrupt cache just means a full sync
	}
	return st
}

func saveState(store *storage.Store, id int64, st syncState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return store.SetSyncState(id, string(raw))
}

func (st *syncState) needsFull(now time.Time) bool {
	return len(st.Collections) == 0 || now.Sub(st.FullAt) >= fullRefreshEvery || now.Before(st.FullAt)
}

const ctagPropfind = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/"><d:prop><cs:getctag/><d:sync-token/></d:prop></d:propfind>`

type ctagMultistatus struct {
	Responses []struct {
		Href     string `xml:"DAV: href"`
		Propstat []struct {
			Prop struct {
				CTag      string `xml:"http://calendarserver.org/ns/ getctag"`
				SyncToken string `xml:"DAV: sync-token"`
			} `xml:"DAV: prop"`
		} `xml:"DAV: propstat"`
	} `xml:"DAV: response"`
}

// fetchCTags returns a change tag per collection: getctag, or the RFC 6578 sync-token for servers
// without it. Collections the server reports no tag for are absent and are always re-fetched.
// One Depth:1 PROPFIND on the home set covers all collections; without a home set each one is asked.
func fetchCTags(ctx context.Context, hc webdav.HTTPClient, endpoint string, st *syncState) (map[string]string, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	tags := map[string]string{}
	if st.Home != "" {
		err := propfindCTags(ctx, hc, base, st.Home, "1", tags)
		var te *ThrottledError
		if err == nil || errors.As(err, &te) {
			return tags, err
		}
		log.Printf("caldav: home set PROPFIND failed, asking each collection: %v", err)
	}
	for _, p := range st.Collections {
		if err := propfindCTags(ctx, hc, base, p, "0", tags); err != nil {
			return nil, err
		}
	}
	return tags, nil
}

func propfindCTags(ctx context.Context, hc webdav.HTTPClient, base *url.URL, p, depth string, tags map[string]string) error {
	u := base.ResolveReference(&url.URL{Path: p})
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", u.String(), strings.NewReader(ctagPropfind))
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
		return fmt.Errorf("PROPFIND %s: %s", p, resp.Status)
	}
	var ms ctagMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return fmt.Errorf("PROPFIND %s: %w", p, err)
	}
	for _, r := range ms.Responses {
		hu, err := url.Parse(r.Href)
		if err != nil {
			continue
		}
		key := cleanDir(hu.Path)
		for _, ps := range r.Propstat {
			if tag := ps.Prop.CTag; tag != "" {
				tags[key] = "ctag:" + tag
			} else if tag := ps.Prop.SyncToken; tag != "" && tags[key] == "" {
				tags[key] = "sync:" + tag
			}
		}
	}
	return nil
}
