// Package caldav implements the background CalDAV synchronization engine.
package caldav

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"

	"github.com/Georgy-Garnov/agendling/internal/ics"
	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/recur"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

const (
	requestTimeout = 60 * time.Second
	pastWindow     = 180 * 24 * time.Hour
	futureWindow   = 2 * 365 * 24 * time.Hour

	// Back-off after a rate-limit response without Retry-After: doubles per consecutive hit.
	minBackoff = 10 * time.Minute
	maxBackoff = 4 * time.Hour
	// maxRetryAfter caps an absurd Retry-After so a misbehaving server can't stop sync for good.
	maxRetryAfter = 24 * time.Hour
)

// Status reports sync progress to the UI.
type Status struct {
	SourceID int64
	Running  bool
	Err      error
	// NewEvents are upcoming events that appeared on the server since the previous sync.
	NewEvents []*model.Event
}

// Engine runs one ticker-driven worker per enabled CalDAV source.
// All network I/O happens on background goroutines; the UI is never blocked.
type Engine struct {
	store    *storage.Store
	onStatus func(Status)

	mu      sync.Mutex
	cancel  context.CancelFunc
	running map[string]*sync.Mutex // per-account lock: sources of one account never sync in parallel
	trigger map[int64]chan struct{}
	blocked map[string]time.Time // per-account rate-limit pause
	strikes map[string]int       // consecutive rate-limit responses per account
}

func NewEngine(store *storage.Store, onStatus func(Status)) *Engine {
	if onStatus == nil {
		onStatus = func(Status) {}
	}
	return &Engine{store: store, onStatus: onStatus, running: map[string]*sync.Mutex{},
		blocked: map[string]time.Time{}, strikes: map[string]int{}}
}

// Restart (re)launches workers according to the current source configuration.
func (e *Engine) Restart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	e.trigger = map[int64]chan struct{}{}

	sources, err := e.store.Sources()
	if err != nil {
		log.Printf("sync: load sources: %v", err)
		return
	}
	for _, src := range sources {
		if src.Type != model.SourceCalDAV || !src.Enabled {
			continue
		}
		ch := make(chan struct{}, 1)
		e.trigger[src.ID] = ch
		go e.worker(ctx, src.ID, src.SyncInterval, ch)
	}
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
}

// SyncNow requests an immediate sync of all enabled CalDAV sources.
func (e *Engine) SyncNow() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, ch := range e.trigger {
		select {
		case ch <- struct{}{}:
		default: // a sync is already queued
		}
	}
}

// SyncSource requests an immediate sync of a single source (e.g. after a local edit).
func (e *Engine) SyncSource(id int64) {
	e.mu.Lock()
	ch := e.trigger[id]
	e.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (e *Engine) worker(ctx context.Context, id int64, intervalMin int, trigger <-chan struct{}) {
	var tick <-chan time.Time
	if intervalMin > 0 {
		t := time.NewTicker(time.Duration(intervalMin) * time.Minute)
		defer t.Stop()
		tick = t.C
	}
	e.runOnce(ctx, id) // initial sync on start-up / reconfiguration
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
		case <-trigger:
		}
		e.runOnce(ctx, id)
	}
}

// accountKey groups sources that talk to the same server account; rate limits apply per account.
func accountKey(src *model.CalendarSource) string {
	host := src.URL
	if u, err := url.Parse(src.URL); err == nil && u.Host != "" {
		host = u.Host
	}
	return strings.ToLower(host) + "|" + src.Username
}

func (e *Engine) lockFor(acct string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.running[acct]
	if m == nil {
		m = &sync.Mutex{}
		e.running[acct] = m
	}
	return m
}

// throttled records a rate-limit response and returns when the account may be contacted again.
func (e *Engine) throttled(acct string, te *ThrottledError, now time.Time) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.strikes[acct]++
	wait := min(maxBackoff, minBackoff<<min(e.strikes[acct]-1, 10))
	if te.RetryAfter > 0 {
		wait = min(te.RetryAfter, maxRetryAfter)
	}
	until := now.Add(wait)
	if until.After(e.blocked[acct]) {
		e.blocked[acct] = until
	}
	return e.blocked[acct]
}

func (e *Engine) blockedUntil(acct string) time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.blocked[acct]
}

func (e *Engine) succeeded(acct string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.strikes, acct)
	delete(e.blocked, acct)
}

func (e *Engine) runOnce(ctx context.Context, id int64) {
	src, err := e.store.Source(id)
	if err != nil || !src.Enabled || src.Type != model.SourceCalDAV {
		return
	}
	acct := accountKey(src)
	lock := e.lockFor(acct)
	lock.Lock()
	defer lock.Unlock()

	defer func() {
		// Never let a malformed server response take the whole app down.
		if r := recover(); r != nil {
			err := fmt.Errorf("sync panic: %v", r)
			_ = e.store.SetSyncStatus(id, "", time.Now(), err)
			e.onStatus(Status{SourceID: id, Err: err})
		}
	}()

	// Skip syncs (timer, local edits, "Sync now") while the server asked us to back off;
	// pending local changes stay dirty and are pushed once the pause is over.
	now := time.Now()
	until := e.blockedUntil(acct)
	if b := loadState(e.store, id).BlockedUntil; b.After(until) {
		until = b // survives an app restart
	}
	if now.Before(until) {
		err := fmt.Errorf("server rate limit: sync paused until %s", until.Local().Format("15:04"))
		_ = e.store.SetSyncStatus(id, "", now, err)
		e.onStatus(Status{SourceID: id, Err: err})
		return
	}

	e.onStatus(Status{SourceID: id, Running: true})
	added, err := SyncSource(ctx, e.store, src)
	if ctx.Err() != nil {
		return
	}
	_ = e.store.SetSyncStatus(id, src.CalendarPath, time.Now(), err)
	var te *ThrottledError
	switch {
	case errors.As(err, &te):
		until := e.throttled(acct, te, time.Now())
		st := loadState(e.store, id)
		st.BlockedUntil = until
		_ = saveState(e.store, id, st)
		log.Printf("sync %q: %v; paused until %s", src.Name, err, until.Format(time.RFC3339))
	case err != nil:
		log.Printf("sync %q: %v", src.Name, err)
	default:
		e.succeeded(acct)
	}
	e.onStatus(Status{SourceID: id, Err: err, NewEvents: added})
	// Parsing a large REPORT response inflates the heap; hand it back to the OS right away
	// instead of waiting for the lazy scavenger, keeping the idle footprint small.
	debug.FreeOSMemory()
}

// newHTTPClient builds the authenticated HTTP stack shared by go-webdav and raw requests.
func newHTTPClient(src *model.CalendarSource) webdav.HTTPClient {
	httpClient := &http.Client{Timeout: requestTimeout}
	auth := webdav.HTTPClientWithBasicAuth(httpClient, src.Username, src.Password)
	return etagFixClient{politeClient{auth}}
}

// NewClient builds an authenticated CalDAV client for a source.
func NewClient(src *model.CalendarSource) (*caldav.Client, error) {
	return caldav.NewClient(newHTTPClient(src), src.URL)
}

// Discover resolves the event collections of a source and the collection used for new events.
func Discover(ctx context.Context, c *caldav.Client, src *model.CalendarSource) (collections []string, writePath string, err error) {
	_, collections, err = discover(ctx, c, src)
	if err != nil {
		return nil, "", err
	}
	return collections, writePathFor(collections, src.CalendarPath), nil
}

// writePathFor keeps the configured write collection when it still exists, else uses the first one.
func writePathFor(collections []string, configured string) string {
	for _, p := range collections {
		if p == configured {
			return p
		}
	}
	return collections[0]
}

// discover finds the calendar home set (empty when the URL is a bare collection) and the event collections.
func discover(ctx context.Context, c *caldav.Client, src *model.CalendarSource) (home string, collections []string, err error) {
	u, err := url.Parse(src.URL)
	if err != nil {
		return "", nil, fmt.Errorf("invalid URL: %w", err)
	}
	direct := cleanDir(u.Path)

	var cals []caldav.Calendar
	principal, perr := c.FindCurrentUserPrincipal(ctx)
	if perr == nil {
		if home, perr = c.FindCalendarHomeSet(ctx, principal); perr == nil {
			cals, perr = c.FindCalendars(ctx, home)
		}
	}
	var te *ThrottledError
	if errors.As(perr, &te) {
		return "", nil, perr // not a discovery problem; the engine backs off
	}
	if perr != nil || len(cals) == 0 {
		// Server without discovery support: treat the URL as the collection itself.
		if direct == "/" {
			if perr == nil {
				perr = errors.New("no calendars found")
			}
			return "", nil, fmt.Errorf("calendar discovery failed: %w", perr)
		}
		return "", []string{direct}, nil
	}

	for _, cal := range cals {
		if cleanDir(cal.Path) == direct {
			return "", []string{cal.Path}, nil // URL points to a specific calendar
		}
	}
	for _, cal := range cals {
		if supportsEvents(cal) {
			collections = append(collections, cal.Path)
		}
	}
	if len(collections) == 0 {
		return "", nil, errors.New("account has no event calendars")
	}
	return home, collections, nil
}

func supportsEvents(cal caldav.Calendar) bool {
	if len(cal.SupportedComponentSet) == 0 {
		return true
	}
	for _, c := range cal.SupportedComponentSet {
		if strings.EqualFold(c, ical.CompEvent) {
			return true
		}
	}
	return false
}

func cleanDir(p string) string {
	if p == "" {
		return "/"
	}
	p = path.Clean(p)
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// SyncSource pushes pending local changes and then pulls the remote state.
// It returns upcoming events that were not known locally before this sync
// (none on a source's first sync, so an initial import isn't reported as "new").
//
// Discovery results are cached and collections are only re-fetched when their change tag
// moved, so a sync of an unchanged account is a single PROPFIND. Once per fullRefreshEvery
// everything is rediscovered and re-fetched.
func SyncSource(ctx context.Context, store *storage.Store, src *model.CalendarSource) ([]*model.Event, error) {
	hc := newHTTPClient(src)
	c, err := caldav.NewClient(hc, src.URL)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	st := loadState(store, src.ID)
	full := st.needsFull(now)
	if full {
		home, collections, err := discover(ctx, c, src)
		if err != nil {
			return nil, err
		}
		st = syncState{Home: home, Collections: collections}
	}
	src.CalendarPath = writePathFor(st.Collections, src.CalendarPath)

	pushErr := push(ctx, c, store, src)
	var te *ThrottledError
	if errors.As(pushErr, &te) {
		return nil, pushErr
	}

	tags, err := fetchCTags(ctx, hc, src.URL, &st)
	if errors.As(err, &te) {
		return nil, err
	}
	if err != nil {
		log.Printf("sync %q: change tags unavailable, fetching everything: %v", src.Name, err)
		tags = map[string]string{}
	}
	var changed []string
	for _, coll := range st.Collections {
		tag := tags[cleanDir(coll)]
		if full || tag == "" || tag != st.CTags[cleanDir(coll)] {
			changed = append(changed, coll)
		}
	}
	if len(changed) == 0 {
		return nil, errors.Join(pushErr, saveState(store, src.ID, st))
	}

	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{Name: ical.CompCalendar, AllProps: true, AllComps: true},
		CompFilter: caldav.CompFilter{
			Name:  ical.CompCalendar,
			Comps: []caldav.CompFilter{{Name: ical.CompEvent, Start: now.Add(-pastWindow), End: now.Add(futureWindow)}},
		},
	}
	var events []*model.Event
	for _, coll := range changed {
		objs, err := c.QueryCalendar(ctx, coll, query)
		if err != nil {
			if isNotFound(err) {
				_ = saveState(store, src.ID, syncState{}) // collection gone: rediscover next time
			}
			return nil, fmt.Errorf("query %s: %w", coll, err)
		}
		for _, obj := range objs {
			if obj.Data == nil {
				continue
			}
			events = append(events, ics.ToEvents(obj.Data, obj.Path, obj.ETag)...)
		}
	}

	known, err := store.KnownUIDs(src.ID)
	if err != nil {
		return nil, err
	}
	var added []*model.Event
	if !src.LastSync.IsZero() || len(known) > 0 {
		added = newUpcoming(events, known, now)
	}
	if full {
		err = store.ReplaceRemoteEvents(src.ID, events)
	} else {
		prefixes := make([]string, len(changed))
		for i, coll := range changed {
			prefixes[i] = cleanDir(coll)
		}
		err = store.ReplaceCollectionEvents(src.ID, prefixes, events)
	}
	if err != nil {
		return nil, fmt.Errorf("store events: %w", err)
	}

	newTags := map[string]string{}
	for _, coll := range st.Collections {
		if tag := tags[cleanDir(coll)]; tag != "" {
			newTags[cleanDir(coll)] = tag
		}
	}
	st.CTags = newTags
	if full {
		st.FullAt = now
	}
	st.BlockedUntil = time.Time{}
	return added, errors.Join(pushErr, saveState(store, src.ID, st))
}

// newUpcoming picks one event per unknown UID (the series master when present)
// that still has an occurrence in the future.
func newUpcoming(events []*model.Event, known map[string]bool, now time.Time) []*model.Event {
	byUID := map[string]*model.Event{}
	var order []string
	for _, e := range events {
		if known[e.UID] {
			continue
		}
		prev, seen := byUID[e.UID]
		if !seen {
			order = append(order, e.UID)
		}
		if !seen || (e.RecurrenceID.IsZero() && !prev.RecurrenceID.IsZero()) {
			byUID[e.UID] = e
		}
	}
	var res []*model.Event
	for _, uid := range order {
		e := byUID[uid]
		if len(recur.Expand([]*model.Event{e}, now, now.Add(futureWindow))) > 0 {
			res = append(res, e)
		}
	}
	return res
}

func push(ctx context.Context, c *caldav.Client, store *storage.Store, src *model.CalendarSource) error {
	pending, err := store.PendingEvents(src.ID)
	if err != nil {
		return err
	}
	done := map[string]bool{}
	var errs []error
	for _, p := range pending {
		if done[p.UID] {
			continue
		}
		done[p.UID] = true
		series, err := store.EventsByUID(src.ID, p.UID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if err := pushSeries(ctx, c, store, src, series); err != nil {
			errs = append(errs, fmt.Errorf("push %q: %w", p.Title, err))
		}
	}
	return errors.Join(errs...)
}

func pushSeries(ctx context.Context, c *caldav.Client, store *storage.Store, src *model.CalendarSource, series []*model.Event) error {
	var master *model.Event
	var live []*model.Event
	for _, ev := range series {
		if ev.RecurrenceID.IsZero() {
			master = ev
		}
		if !ev.Deleted {
			live = append(live, ev)
		}
	}
	href := ""
	for _, ev := range series {
		if ev.Href != "" {
			href = ev.Href
			break
		}
	}

	if master == nil || master.Deleted {
		// Whole series deleted locally.
		if href != "" {
			if err := c.RemoveAll(ctx, href); err != nil && !isNotFound(err) {
				return err
			}
		}
		return store.PurgeUID(src.ID, series[0].UID)
	}

	if href == "" {
		href = path.Join(src.CalendarPath, sanitize(master.UID)+".ics")
	}
	obj, err := c.PutCalendarObject(ctx, href, ics.FromEvents(live))
	if err != nil {
		return err
	}
	for _, ev := range series {
		if ev.Deleted {
			if err := store.PurgeEvent(ev.ID); err != nil {
				return err
			}
			continue
		}
		ev.Href, ev.ETag, ev.Dirty = href, obj.ETag, false
		if err := store.SaveEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

func isNotFound(err error) bool {
	// go-webdav keeps its HTTPError type internal; its message carries the status code.
	msg := err.Error()
	return strings.Contains(msg, "404") || strings.Contains(msg, "410")
}

func sanitize(uid string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, uid)
}

// TestConnection verifies credentials and discovery for the Settings dialog.
func TestConnection(ctx context.Context, src *model.CalendarSource) (int, error) {
	c, err := NewClient(src)
	if err != nil {
		return 0, err
	}
	colls, _, err := Discover(ctx, c, src)
	return len(colls), err
}
