// Package storage persists calendar sources, events and settings in an embedded
// SQLite database (modernc.org/sqlite, no CGO).
package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/secret"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sources (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	name          TEXT NOT NULL,
	type          TEXT NOT NULL,
	url           TEXT NOT NULL DEFAULT '',
	username      TEXT NOT NULL DEFAULT '',
	password      TEXT NOT NULL DEFAULT '',
	color         TEXT NOT NULL DEFAULT '#3a87ad',
	sync_interval INTEGER NOT NULL DEFAULT 15,
	enabled       INTEGER NOT NULL DEFAULT 1,
	calendar_path TEXT NOT NULL DEFAULT '',
	last_sync     INTEGER NOT NULL DEFAULT 0,
	last_error    TEXT NOT NULL DEFAULT '',
	sync_state    TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS events (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	calendar_id   INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
	uid           TEXT NOT NULL,
	title         TEXT NOT NULL DEFAULT '',
	description   TEXT NOT NULL DEFAULT '',
	location      TEXT NOT NULL DEFAULT '',
	url           TEXT NOT NULL DEFAULT '',
	start_ts      INTEGER NOT NULL,
	end_ts        INTEGER NOT NULL,
	all_day       INTEGER NOT NULL DEFAULT 0,
	rrule         TEXT NOT NULL DEFAULT '',
	exdates       TEXT NOT NULL DEFAULT '',
	reminders     TEXT NOT NULL DEFAULT '',
	recurrence_id INTEGER NOT NULL DEFAULT 0,
	tzid          TEXT NOT NULL DEFAULT '',
	href          TEXT NOT NULL DEFAULT '',
	etag          TEXT NOT NULL DEFAULT '',
	dirty         INTEGER NOT NULL DEFAULT 0,
	deleted       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_events_cal   ON events(calendar_id);
CREATE INDEX IF NOT EXISTS idx_events_range ON events(start_ts, end_ts);
CREATE INDEX IF NOT EXISTS idx_events_uid   ON events(calendar_id, uid);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS fired_reminders (
	event_uid  TEXT NOT NULL,
	occ_start  INTEGER NOT NULL,
	offset_min INTEGER NOT NULL,
	fired_at   INTEGER NOT NULL,
	PRIMARY KEY (event_uid, occ_start, offset_min)
);
`

// migrate adds columns introduced after the first release.
func migrate(db *sql.DB) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('events') WHERE name = 'tzid'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE events ADD COLUMN tzid TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sources') WHERE name = 'sync_state'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := db.Exec(`ALTER TABLE sources ADD COLUMN sync_state TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

// Store is safe for concurrent use.
type Store struct {
	db *sql.DB
	mu sync.Mutex // serializes writes; SQLite allows a single writer

	listenersMu sync.Mutex
	listeners   []func()
}

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // keeps memory low and avoids writer contention
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// OnChange registers a callback invoked (from the writer goroutine) after any data change.
func (s *Store) OnChange(fn func()) {
	s.listenersMu.Lock()
	s.listeners = append(s.listeners, fn)
	s.listenersMu.Unlock()
}

func (s *Store) notify() {
	s.listenersMu.Lock()
	ls := append([]func(){}, s.listeners...)
	s.listenersMu.Unlock()
	for _, fn := range ls {
		fn()
	}
}

// ---------- Sources ----------

const sourceCols = `id, name, type, url, username, password, color, sync_interval, enabled, calendar_path, last_sync, last_error`

func scanSource(sc interface{ Scan(...any) error }) (*model.CalendarSource, error) {
	var src model.CalendarSource
	var typ, pw string
	var enabled int
	var lastSync int64
	if err := sc.Scan(&src.ID, &src.Name, &typ, &src.URL, &src.Username, &pw, &src.Color,
		&src.SyncInterval, &enabled, &src.CalendarPath, &lastSync, &src.LastError); err != nil {
		return nil, err
	}
	src.Type = model.SourceType(typ)
	src.Enabled = enabled != 0
	if lastSync > 0 {
		src.LastSync = time.Unix(lastSync, 0)
	}
	plain, err := secret.Decrypt(pw)
	if err != nil {
		src.LastError = "cannot decrypt password: " + err.Error()
	}
	src.Password = plain
	return &src, nil
}

func (s *Store) Sources() ([]*model.CalendarSource, error) {
	rows, err := s.db.Query(`SELECT ` + sourceCols + ` FROM sources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*model.CalendarSource
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, src)
	}
	return res, rows.Err()
}

func (s *Store) Source(id int64) (*model.CalendarSource, error) {
	return scanSource(s.db.QueryRow(`SELECT `+sourceCols+` FROM sources WHERE id = ?`, id))
}

func (s *Store) SaveSource(src *model.CalendarSource) error {
	pw, err := secret.Encrypt(src.Password)
	if err != nil {
		return fmt.Errorf("encrypt password: %w", err)
	}
	s.mu.Lock()
	if src.ID == 0 {
		var res sql.Result
		res, err = s.db.Exec(`INSERT INTO sources (name, type, url, username, password, color, sync_interval, enabled, calendar_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			src.Name, string(src.Type), src.URL, src.Username, pw, src.Color, src.SyncInterval, b2i(src.Enabled), src.CalendarPath)
		if err == nil {
			src.ID, err = res.LastInsertId()
		}
	} else {
		// Cached sync state belongs to the old account when the server or login changes.
		_, err = s.db.Exec(`UPDATE sources SET sync_state = CASE WHEN url=? AND username=? THEN sync_state ELSE '' END,
			name=?, type=?, url=?, username=?, password=?, color=?, sync_interval=?, enabled=?, calendar_path=?
			WHERE id=?`,
			src.URL, src.Username,
			src.Name, string(src.Type), src.URL, src.Username, pw, src.Color, src.SyncInterval, b2i(src.Enabled), src.CalendarPath, src.ID)
	}
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// SetSyncStatus records the outcome of a sync without touching other fields.
func (s *Store) SetSyncStatus(id int64, calendarPath string, at time.Time, syncErr error) error {
	msg := ""
	if syncErr != nil {
		msg = syncErr.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if syncErr != nil {
		_, err := s.db.Exec(`UPDATE sources SET last_error=? WHERE id=?`, msg, id)
		return err
	}
	_, err := s.db.Exec(`UPDATE sources SET calendar_path=?, last_sync=?, last_error='' WHERE id=?`, calendarPath, at.Unix(), id)
	return err
}

// SyncState returns the opaque sync engine state of a source ("" when none is cached).
func (s *Store) SyncState(id int64) (string, error) {
	var st string
	err := s.db.QueryRow(`SELECT sync_state FROM sources WHERE id=?`, id).Scan(&st)
	return st, err
}

// SetSyncState stores the opaque sync engine state of a source.
func (s *Store) SetSyncState(id int64, st string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE sources SET sync_state=? WHERE id=?`, st, id)
	return err
}

func (s *Store) DeleteSource(id int64) error {
	s.mu.Lock()
	_, err := s.db.Exec(`DELETE FROM sources WHERE id=?`, id)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// ---------- Events ----------

const eventCols = `id, calendar_id, uid, title, description, location, url, start_ts, end_ts, all_day, rrule, exdates, reminders, recurrence_id, href, etag, dirty, deleted, tzid`

func scanEvent(sc interface{ Scan(...any) error }, localCals map[int64]bool) (*model.Event, error) {
	var e model.Event
	var startTS, endTS, recID int64
	var allDay, dirty, deleted int
	var exdates, reminders string
	if err := sc.Scan(&e.ID, &e.CalendarID, &e.UID, &e.Title, &e.Description, &e.Location, &e.URL,
		&startTS, &endTS, &allDay, &e.RRule, &exdates, &reminders, &recID, &e.Href, &e.ETag, &dirty, &deleted, &e.TZID); err != nil {
		return nil, err
	}
	e.AllDay = allDay != 0
	e.StartTime = fromTS(startTS, e.AllDay)
	e.EndTime = fromTS(endTS, e.AllDay)
	if recID != 0 {
		e.RecurrenceID = fromTS(recID, e.AllDay)
	}
	e.ExDates = decodeTimes(exdates, e.AllDay)
	e.Reminders = decodeReminders(reminders)
	e.Dirty = dirty != 0
	e.Deleted = deleted != 0
	e.IsLocal = localCals[e.CalendarID]
	return &e, nil
}

func (s *Store) localCalendarIDs() map[int64]bool {
	m := map[int64]bool{}
	rows, err := s.db.Query(`SELECT id FROM sources WHERE type = ?`, string(model.SourceLocal))
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			m[id] = true
		}
	}
	return m
}

func (s *Store) queryEvents(where string, args ...any) ([]*model.Event, error) {
	local := s.localCalendarIDs()
	rows, err := s.db.Query(`SELECT `+eventCols+` FROM events WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []*model.Event
	for rows.Next() {
		e, err := scanEvent(rows, local)
		if err != nil {
			return nil, err
		}
		res = append(res, e)
	}
	return res, rows.Err()
}

// EventsInRange returns candidate events for [from, to) from enabled calendars:
// single events overlapping the range, all recurring masters starting before `to`,
// and all overrides (needed to suppress replaced occurrences).
func (s *Store) EventsInRange(from, to time.Time) ([]*model.Event, error) {
	// All-day events are stored as UTC midnight, so widen by a day to be timezone-safe.
	f := from.Add(-24 * time.Hour).Unix()
	t := to.Add(24 * time.Hour).Unix()
	return s.queryEvents(`deleted = 0
		AND calendar_id IN (SELECT id FROM sources WHERE enabled = 1)
		AND ((rrule = '' AND start_ts < ? AND end_ts >= ?) OR (rrule <> '' AND start_ts < ?) OR recurrence_id <> 0)`,
		t, f, t)
}

func (s *Store) Event(id int64) (*model.Event, error) {
	evs, err := s.queryEvents(`id = ?`, id)
	if err != nil {
		return nil, err
	}
	if len(evs) == 0 {
		return nil, sql.ErrNoRows
	}
	return evs[0], nil
}

// EventsByUID returns the master and overrides of a series (including locally deleted rows).
func (s *Store) EventsByUID(calendarID int64, uid string) ([]*model.Event, error) {
	return s.queryEvents(`calendar_id = ? AND uid = ? ORDER BY recurrence_id`, calendarID, uid)
}

// KnownUIDs returns the UIDs of all events stored for a calendar (including pending deletes).
func (s *Store) KnownUIDs(calendarID int64) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT uid FROM events WHERE calendar_id = ?`, calendarID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := map[string]bool{}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		res[uid] = true
	}
	return res, rows.Err()
}

// PendingEvents returns locally modified or deleted events of a calendar awaiting push.
func (s *Store) PendingEvents(calendarID int64) ([]*model.Event, error) {
	return s.queryEvents(`calendar_id = ? AND (dirty = 1 OR deleted = 1)`, calendarID)
}

func (s *Store) SaveEvent(e *model.Event) error {
	s.mu.Lock()
	err := s.saveEventLocked(s.db, e)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (s *Store) saveEventLocked(x execer, e *model.Event) error {
	var recID int64
	if !e.RecurrenceID.IsZero() {
		recID = toTS(e.RecurrenceID, e.AllDay)
	}
	args := []any{e.CalendarID, e.UID, e.Title, e.Description, e.Location, e.URL,
		toTS(e.StartTime, e.AllDay), toTS(e.EndTime, e.AllDay), b2i(e.AllDay), e.RRule,
		encodeTimes(e.ExDates, e.AllDay), encodeReminders(e.Reminders), recID, e.Href, e.ETag, b2i(e.Dirty), b2i(e.Deleted), e.TZID}
	if e.ID == 0 {
		res, err := x.Exec(`INSERT INTO events (calendar_id, uid, title, description, location, url, start_ts, end_ts, all_day,
			rrule, exdates, reminders, recurrence_id, href, etag, dirty, deleted, tzid) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
		if err != nil {
			return err
		}
		e.ID, err = res.LastInsertId()
		return err
	}
	_, err := x.Exec(`UPDATE events SET calendar_id=?, uid=?, title=?, description=?, location=?, url=?, start_ts=?, end_ts=?, all_day=?,
		rrule=?, exdates=?, reminders=?, recurrence_id=?, href=?, etag=?, dirty=?, deleted=?, tzid=? WHERE id=?`, append(args, e.ID)...)
	return err
}

// PurgeEvent removes an event row permanently (after a successful remote delete, or for local calendars).
func (s *Store) PurgeEvent(id int64) error {
	s.mu.Lock()
	_, err := s.db.Exec(`DELETE FROM events WHERE id=?`, id)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// PurgeUID removes a whole series (master + overrides) of a calendar.
func (s *Store) PurgeUID(calendarID int64, uid string) error {
	s.mu.Lock()
	_, err := s.db.Exec(`DELETE FROM events WHERE calendar_id=? AND uid=?`, calendarID, uid)
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// ReplaceRemoteEvents atomically replaces all clean (non-dirty, non-deleted) events of a
// calendar with the freshly fetched set. Pending local changes are preserved.
func (s *Store) ReplaceRemoteEvents(calendarID int64, events []*model.Event) error {
	return s.replaceRemote(calendarID, nil, events)
}

// ReplaceCollectionEvents is ReplaceRemoteEvents limited to the server collections with the
// given path prefixes; clean events stored under other collections are kept.
func (s *Store) ReplaceCollectionEvents(calendarID int64, collections []string, events []*model.Event) error {
	return s.replaceRemote(calendarID, collections, events)
}

// replaceRemote replaces clean events of the given collections, or of the whole calendar when nil.
func (s *Store) replaceRemote(calendarID int64, collections []string, events []*model.Event) error {
	s.mu.Lock()
	err := func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		pending := map[string]bool{}
		rows, err := tx.Query(`SELECT uid FROM events WHERE calendar_id=? AND (dirty=1 OR deleted=1)`, calendarID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var uid string
			if rows.Scan(&uid) == nil {
				pending[uid] = true
			}
		}
		rows.Close()

		if collections == nil {
			if _, err := tx.Exec(`DELETE FROM events WHERE calendar_id=? AND dirty=0 AND deleted=0`, calendarID); err != nil {
				return err
			}
		}
		for _, coll := range collections {
			if _, err := tx.Exec(`DELETE FROM events WHERE calendar_id=? AND dirty=0 AND deleted=0 AND instr(href, ?)=1`,
				calendarID, coll); err != nil {
				return err
			}
		}
		for _, e := range events {
			if pending[e.UID] {
				continue // local edit wins until pushed
			}
			e.ID = 0
			e.CalendarID = calendarID
			if err := s.saveEventLocked(tx, e); err != nil {
				return err
			}
		}
		return tx.Commit()
	}()
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// ---------- Settings ----------

func (s *Store) Settings() model.AppSettings {
	st := model.DefaultSettings()
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='app'`).Scan(&raw); err == nil {
		_ = json.Unmarshal([]byte(raw), &st)
	}
	if st.SnoozeIntervalMinutes <= 0 {
		st.SnoozeIntervalMinutes = 5
	}
	return st
}

func (s *Store) SaveSettings(st model.AppSettings) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	s.mu.Lock()
	_, err = s.db.Exec(`INSERT INTO settings(key, value) VALUES('app', ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b))
	s.mu.Unlock()
	if err == nil {
		s.notify()
	}
	return err
}

// ---------- Fired reminders ----------

func (s *Store) WasFired(uid string, occStart time.Time, offsetMin int) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM fired_reminders WHERE event_uid=? AND occ_start=? AND offset_min=?`,
		uid, occStart.Unix(), offsetMin).Scan(&n)
	return n > 0
}

func (s *Store) MarkFired(uid string, occStart time.Time, offsetMin int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT OR IGNORE INTO fired_reminders(event_uid, occ_start, offset_min, fired_at) VALUES (?,?,?,?)`,
		uid, occStart.Unix(), offsetMin, time.Now().Unix())
	return err
}

// PruneFired drops bookkeeping for occurrences older than the cutoff.
func (s *Store) PruneFired(before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM fired_reminders WHERE occ_start < ?`, before.Unix())
	return err
}

// ---------- helpers ----------

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// All-day dates are stored as UTC midnight of the calendar date so they stay on the same
// date regardless of the machine timezone; timed events are stored as absolute instants.
func toTS(t time.Time, allDay bool) int64 {
	if allDay {
		y, m, d := t.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix()
	}
	return t.Unix()
}

func fromTS(ts int64, allDay bool) time.Time {
	if allDay {
		y, m, d := time.Unix(ts, 0).UTC().Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
	}
	return time.Unix(ts, 0).Local()
}

func encodeTimes(ts []time.Time, allDay bool) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = strconv.FormatInt(toTS(t, allDay), 10)
	}
	return strings.Join(parts, ",")
}

func decodeTimes(s string, allDay bool) []time.Time {
	if s == "" {
		return nil
	}
	var res []time.Time
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.ParseInt(p, 10, 64); err == nil {
			res = append(res, fromTS(v, allDay))
		}
	}
	return res
}

func encodeReminders(rs []model.ReminderOffset) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		parts[i] = strconv.Itoa(r.Minutes())
	}
	return strings.Join(parts, ",")
}

func decodeReminders(s string) []model.ReminderOffset {
	if s == "" {
		return nil
	}
	var res []model.ReminderOffset
	for _, p := range strings.Split(s, ",") {
		if v, err := strconv.Atoi(p); err == nil {
			res = append(res, model.MinutesOffset(v))
		}
	}
	return res
}
