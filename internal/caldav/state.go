package caldav

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"time"

	"github.com/emersion/go-webdav"

	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// fullRefreshEvery bounds how long discovery results and change tags are trusted: a full pull
// also slides the query window and drops collections that disappeared from the server.
const fullRefreshEvery = 24 * time.Hour

// stateVersion invalidates caches written by builds that fetched collections differently
// (2: partition hosts and plain calendar-data; earlier caches may hold tags of empty pulls).
const stateVersion = 2

// syncState is cached per source between syncs so an unchanged account costs a single PROPFIND.
type syncState struct {
	Version int `json:"v,omitempty"`
	// Endpoint is the server root all paths below refer to, when discovery moved to another
	// host than the configured URL (iCloud account partitions); "" means the configured URL.
	Endpoint     string            `json:"endpoint,omitempty"`
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

func (st *syncState) endpoint(src string) string {
	if st.Endpoint != "" {
		return st.Endpoint
	}
	return src
}

func (st *syncState) needsFull(now time.Time) bool {
	return st.Version != stateVersion || len(st.Collections) == 0 || now.Sub(st.FullAt) >= fullRefreshEvery || now.Before(st.FullAt)
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
	var ms ctagMultistatus
	if err := propfind(ctx, hc, base.ResolveReference(&url.URL{Path: p}), depth, ctagPropfind, &ms); err != nil {
		return err
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
