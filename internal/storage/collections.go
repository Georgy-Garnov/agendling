package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/secret"
)

// collectionKey compares collection identities, not account display names.
func collectionKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = ""
	u.Fragment = ""
	return u.String()
}

func sourceCollection(src *model.CalendarSource) string {
	if src.CollectionURL != "" {
		return collectionKey(src.CollectionURL)
	}
	return collectionKey(src.URL)
}

// ReconcileCalDAVSource retains account discovery and pins every calendar. It reuses
// matching subscriptions and moves events in the same transaction, including local edits.
func (s *Store) ReconcileCalDAVSource(src *model.CalendarSource, calendars []*model.CalendarSource) error {
	if len(calendars) == 0 {
		return fmt.Errorf("at least one calendar is required")
	}
	seen := map[string]bool{}
	for _, cal := range calendars {
		if cal.CalendarPath == "" || !strings.HasPrefix(cal.CalendarPath, "/") || !strings.HasSuffix(cal.CalendarPath, "/") {
			return fmt.Errorf("invalid collection path")
		}
		key := collectionKey(cal.CollectionURL)
		if cal.CollectionURL == "" || seen[key] {
			return fmt.Errorf("invalid or duplicate collection URL")
		}
		seen[key] = true
	}
	s.mu.Lock()
	err := func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		rows, err := tx.Query(`SELECT ` + sourceCols + ` FROM sources ORDER BY id`)
		if err != nil {
			return err
		}
		var existing []*model.CalendarSource
		for rows.Next() {
			cal, err := scanSource(rows)
			if err != nil {
				rows.Close()
				return err
			}
			existing = append(existing, cal)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		var parent *model.CalendarSource
		for _, cal := range existing {
			if cal.ID == src.ID {
				parent = cal
				break
			}
		}
		if parent == nil || parent.Revision != src.Revision {
			return ErrSourceChanged
		}

		type merge struct{ from, to int64 }
		var merges []merge
		for i, desired := range calendars {
			var matches []*model.CalendarSource
			for _, old := range existing {
				if old.ID != src.ID && old.Type == model.SourceCalDAV && old.Username == src.Username && sourceCollection(old) == collectionKey(desired.CollectionURL) {
					matches = append(matches, old)
				}
			}
			if i == 0 {
				desired.ID = src.ID
				desired.Revision = parent.Revision
				if len(matches) > 0 {
					// Preserve explicitly configured preferences if the old account's default
					// already has a direct subscription. Keep the original account row as owner.
					configured := *matches[0]
					configured.ID, configured.Revision = desired.ID, desired.Revision
					configured.CollectionURL, configured.CalendarPath = desired.CollectionURL, desired.CalendarPath
					configured.DiscoveryURL, configured.DiscoveryName = desired.DiscoveryURL, desired.DiscoveryName
					*desired = configured
				}
				if !sameSourceConfig(parent, desired) {
					if err := updateReconciledSource(tx, desired); err != nil {
						return err
					}
				}
			} else if len(matches) > 0 {
				binding := desired.CollectionURL
				*desired = *matches[0]
				if desired.CollectionURL == "" {
					desired.CollectionURL = binding
					u, _ := url.Parse(binding)
					desired.CalendarPath = u.Path
					if err := updateReconciledSource(tx, desired); err != nil {
						return err
					}
				}
			} else {
				pw, err := secret.Encrypt(desired.Password)
				if err != nil {
					return err
				}
				res, err := tx.Exec(`INSERT INTO sources(name,type,url,username,password,color,sync_interval,enabled,calendar_path,last_sync,skip_tls_verify,collection_url)
     VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, desired.Name, string(desired.Type), desired.URL, desired.Username, pw, desired.Color, desired.SyncInterval, b2i(desired.Enabled), desired.CalendarPath, sourceSyncTime(desired), b2i(desired.SkipTLSVerify), desired.CollectionURL)
				if err != nil {
					return err
				}
				desired.ID, err = res.LastInsertId()
				if err != nil {
					return err
				}
				desired.Revision = 1
			}
			for _, old := range matches {
				if old.ID != desired.ID {
					merges = append(merges, merge{old.ID, desired.ID})
				}
			}
		}

		events, err := collectionEventRows(tx, src.ID)
		if err != nil {
			return err
		}
		for _, ev := range events {
			u, err := url.Parse(ev.href)
			if err != nil {
				return err
			}
			// Longest match also handles nested collection paths without misassigning rows.
			target, longest := src.ID, 0
			for _, cal := range calendars {
				if strings.HasPrefix(u.Path, cal.CalendarPath) && len(cal.CalendarPath) > longest {
					target, longest = cal.ID, len(cal.CalendarPath)
				}
			}
			if target != src.ID {
				if err := moveCollectionEvent(tx, ev, target); err != nil {
					return err
				}
			}
		}
		for _, m := range merges {
			events, err := collectionEventRows(tx, m.from)
			if err != nil {
				return err
			}
			for _, ev := range events {
				if err := moveCollectionEvent(tx, ev, m.to); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`DELETE FROM sources WHERE id=?`, m.from); err != nil {
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

func sourceSyncTime(src *model.CalendarSource) int64 {
	if src.LastSync.IsZero() {
		return 0
	}
	return src.LastSync.Unix()
}

func sameSourceConfig(a, b *model.CalendarSource) bool {
	left, right := *a, *b
	left.LastSync, right.LastSync = b.LastSync, b.LastSync
	left.LastError, right.LastError = "", ""
	return reflect.DeepEqual(left, right)
}

func updateReconciledSource(tx *sql.Tx, cal *model.CalendarSource) error {
	pw, err := secret.Encrypt(cal.Password)
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE sources SET name=?,url=?,username=?,password=?,color=?,sync_interval=?,enabled=?,skip_tls_verify=?,calendar_path=?,collection_url=?,discovery_url=?,discovery_name=?,sync_state='',revision=revision+1 WHERE id=? AND revision=?`,
		cal.Name, cal.URL, cal.Username, pw, cal.Color, cal.SyncInterval, b2i(cal.Enabled), b2i(cal.SkipTLSVerify), cal.CalendarPath, cal.CollectionURL, cal.DiscoveryURL, cal.DiscoveryName, cal.ID, cal.Revision)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSourceChanged
	}
	cal.Revision++
	return nil
}

type collectionEvent struct {
	id             int64
	uid, href      string
	recurrence     int64
	dirty, deleted bool
}

func collectionEventRows(tx *sql.Tx, id int64) ([]collectionEvent, error) {
	rows, err := tx.Query(`SELECT id,uid,href,recurrence_id,dirty,deleted FROM events WHERE calendar_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []collectionEvent
	for rows.Next() {
		var ev collectionEvent
		if err := rows.Scan(&ev.id, &ev.uid, &ev.href, &ev.recurrence, &ev.dirty, &ev.deleted); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// Pending edits win over clean duplicates. Two pending copies require an explicit
// resolution: roll back instead of silently discarding either user's change.
func moveCollectionEvent(tx *sql.Tx, ev collectionEvent, target int64) error {
	rows, err := tx.Query(`SELECT id,dirty,deleted FROM events WHERE calendar_id=? AND uid=? AND recurrence_id=?`, target, ev.uid, ev.recurrence)
	if err != nil {
		return err
	}
	var duplicates []int64
	pending := false
	for rows.Next() {
		var id int64
		var dirty, deleted bool
		if err := rows.Scan(&id, &dirty, &deleted); err != nil {
			rows.Close()
			return err
		}
		duplicates = append(duplicates, id)
		pending = pending || dirty || deleted
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(duplicates) > 0 {
		if (ev.dirty || ev.deleted) && pending {
			return fmt.Errorf("two pending copies of event %q; resolve duplicate subscriptions before syncing", ev.uid)
		}
		if !ev.dirty && !ev.deleted {
			_, err = tx.Exec(`DELETE FROM events WHERE id=?`, ev.id)
			return err
		}
		for _, id := range duplicates {
			if _, err = tx.Exec(`DELETE FROM events WHERE id=?`, id); err != nil {
				return err
			}
		}
	}
	_, err = tx.Exec(`UPDATE events SET calendar_id=? WHERE id=?`, target, ev.id)
	return err
}
