package caldav

import (
	"net/url"
	"path"
	"strings"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

// reconcileDiscoveredCalendars keeps account discovery separate from the pinned
// collection. New siblings are added without ever changing an existing write target.
func reconcileDiscoveredCalendars(store *storage.Store, src *model.CalendarSource, d *discovery) error {
	endpoint := src.URL
	if src.CollectionURL != "" {
		endpoint = src.CollectionURL
	}
	if src.DiscoveryURL != "" {
		endpoint = src.DiscoveryURL
	}
	if d.endpoint != "" {
		endpoint = d.endpoint
	}
	base, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	discoveryURL, discoveryName := src.DiscoveryURL, src.DiscoveryName
	if d.confirmed && !d.direct && src.CollectionURL == "" {
		discoveryURL, discoveryName = src.URL, src.Name
	}
	// Version 3 replaced account URLs with collection URLs. Recover one coordinator
	// from the old generated siblings, using their common calendar-home-set.
	if d.confirmed && d.direct && src.CollectionURL == "" && discoveryURL == "" && d.home != "" {
		sources, err := store.Sources()
		if err != nil {
			return err
		}
		prefix, _, generated := strings.Cut(src.Name, " / ")
		if generated {
			sibling, first := false, src.ID
			for _, other := range sources {
				u, err := url.Parse(other.URL)
				if err != nil || other.ID == src.ID || other.Type != model.SourceCalDAV || other.Username != src.Username || !strings.HasPrefix(other.Name, prefix+" / ") {
					continue
				}
				for _, p := range d.allCollections {
					if u.Scheme == base.Scheme && u.Host == base.Host && cleanDir(u.Path) == p {
						sibling = true
						if other.Enabled && other.ID < first {
							first = other.ID
						}
					}
				}
			}
			if sibling && first == src.ID {
				discoveryURL = base.ResolveReference(&url.URL{Path: d.home}).String()
				discoveryName = prefix
			}
		}
	}
	first := src.CalendarPath
	if src.CollectionURL != "" {
		u, err := url.Parse(src.CollectionURL)
		if err != nil {
			return err
		}
		first = cleanDir(u.Path)
	}
	if first == "" {
		first = d.collections[0]
	}
	paths := []string{cleanDir(first)}
	if discoveryURL != "" && d.confirmed {
		for _, p := range d.allCollections {
			if cleanDir(p) != cleanDir(first) {
				paths = append(paths, cleanDir(p))
			}
		}
	}
	palette := []string{"#e67c73", "#3f51b5", "#f6bf26", "#8e24aa", "#039be5"}
	calendars := make([]*model.CalendarSource, 0, len(paths))
	for i, p := range paths {
		cp := *src
		cp.CalendarPath = p
		cp.CollectionURL = base.ResolveReference(&url.URL{Path: p}).String()
		if i == 0 {
			if src.CollectionURL != "" {
				cp.CollectionURL = src.CollectionURL
			}
			cp.DiscoveryURL, cp.DiscoveryName = discoveryURL, discoveryName
			// Only the initial account split changes its display name.
			if src.CollectionURL == "" && discoveryURL != "" && !d.direct {
				cp.Name = collectionName(discoveryName, p, d.names)
			}
		} else {
			cp.ID, cp.Revision = 0, 0
			cp.Enabled = true
			cp.URL = cp.CollectionURL
			cp.DiscoveryURL, cp.DiscoveryName = "", ""
			cp.Name = collectionName(discoveryName, p, d.names)
			cp.Color = palette[(i-1)%len(palette)]
			if cp.Color == src.Color {
				cp.Color = palette[i%len(palette)]
			}
		}
		calendars = append(calendars, &cp)
	}
	if err := store.ReconcileCalDAVSource(src, calendars); err != nil {
		return err
	}
	*src = *calendars[0]
	d.collections = []string{src.CalendarPath}
	return nil
}

func collectionName(account, p string, names map[string]string) string {
	name := strings.TrimSpace(names[cleanDir(p)])
	if name == "" {
		name = path.Base(strings.TrimSuffix(p, "/"))
	}
	return account + " / " + name
}
