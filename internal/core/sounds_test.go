package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Georgy-Garnov/agendling/internal/model"
	"github.com/Georgy-Garnov/agendling/internal/storage"
)

func TestAlertSound(t *testing.T) {
	local := &model.CalendarSource{ID: 1, Type: model.SourceLocal}
	acct := &model.CalendarSource{ID: 2, Type: model.SourceCalDAV, CalendarPath: "/u/a/"}
	personal := &model.Event{CalendarID: 1}
	work := &model.Event{CalendarID: 2, Href: "/u/a/w.ics"}
	home := &model.Event{CalendarID: 2, Href: "/u/b/h.ics"}
	on := func(path string) model.Sound { return model.Sound{Custom: true, Path: path} }

	st := model.DefaultSettings()
	st.CustomSoundPath = "main.wav"
	// Settings from before per-alert sounds: everything plays the main sound.
	for _, isNew := range []bool{false, true} {
		if got := AlertSound(st, home, acct, isNew); got != "main.wav" {
			t.Fatalf("no custom sounds: %q", got)
		}
	}

	st.TypeSounds.NewEvent = on("new.wav")
	st.TypeSounds.Reminder = model.Sound{Path: "off.wav"} // remembered but switched off
	st.CalendarSounds = map[string]model.AlertSounds{
		"c2":        {Reminder: on("account.wav")},
		"c2:/u/b/":  {Reminder: on(""), NewEvent: on("home-new.wav")}, // "" = built-in chime
		"c1":        {NewEvent: model.Sound{Path: "personal-off.wav"}},
		"c9:/gone/": {Reminder: on("gone.wav")},
	}
	for _, tc := range []struct {
		name  string
		e     *model.Event
		src   *model.CalendarSource
		isNew bool
		want  string
	}{
		{"calendar sound", home, acct, true, "home-new.wav"},
		{"calendar sound: built-in chime", home, acct, false, ""},
		{"account sound for its other calendar", work, acct, false, "account.wav"},
		{"kind of alert when the account has none", work, acct, true, "new.wav"},
		{"switched-off sounds fall through", personal, local, false, "main.wav"},
		{"kind of alert for a local calendar", personal, local, true, "new.wav"},
		{"several events: kind of alert", nil, nil, true, "new.wav"},
	} {
		if got := AlertSound(st, tc.e, tc.src, tc.isNew); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSoundSettingsFromOlderVersions(t *testing.T) {
	st := model.DefaultSettings() // as storage.Settings: the saved JSON is read over the defaults
	if err := json.Unmarshal([]byte(`{"CustomSoundPath":"a.mp3","LoopSound":true}`), &st); err != nil {
		t.Fatal(err)
	}
	if st.SoundLimitSeconds != 180 || st.CalendarSounds != nil || st.TypeSounds != (model.AlertSounds{}) {
		t.Fatalf("older settings: %+v", st)
	}
	st.SoundLimitSeconds = 0 // no limit must survive a save
	b, _ := json.Marshal(st)
	again := model.DefaultSettings()
	if err := json.Unmarshal(b, &again); err != nil || again.SoundLimitSeconds != 0 {
		t.Fatalf("no limit lost: %d, %v", again.SoundLimitSeconds, err)
	}
}

func TestCalendarSoundsComparison(t *testing.T) {
	a := model.DefaultSettings()
	b := a
	b.CalendarSounds = map[string]model.AlertSounds{"c1": {}, "c2": {Reminder: model.Sound{Path: "  "}}}
	if !SameEditableSettings(a, b) {
		t.Fatal("empty entries count as a change")
	}
	b.CalendarSounds["c2"] = model.AlertSounds{Reminder: model.Sound{Path: "x.wav"}}
	if SameEditableSettings(a, b) {
		t.Fatal("a remembered file is not a change")
	}
	if got := CleanCalendarSounds(b.CalendarSounds); len(got) != 1 || got["c2"].Reminder.Path != "x.wav" {
		t.Fatalf("clean: %+v", got)
	}
}

func TestSoundTargets(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := NewService(store, nil, nil)
	svc.EnsureDefaultCalendar()
	multi := &model.CalendarSource{Name: "Cloud", Type: model.SourceCalDAV, URL: "https://dav.example/", Username: "u",
		Enabled: true, CalendarPath: "/u/a/", HiddenCollections: []string{"/u/c/"}}
	single := &model.CalendarSource{Name: "Work", Type: model.SourceCalDAV, URL: "https://work.example/", Username: "w",
		CalendarPath: "/w/cal/"}
	for _, src := range []*model.CalendarSource{multi, single} {
		if err := store.SaveSource(src); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetSyncState(multi.ID, `{"v":3,"collections":["/u/a/","/u/b/","/u/c/"],"names":{"/u/a/":"Work","/u/b/":"Home","/u/c/":"Old"}}`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSyncState(single.ID, `{"v":3,"collections":["/w/cal/"],"names":{"/w/cal/":"Calendar"}}`); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, st := range svc.SoundTargets() {
		indent := ""
		if st.Child {
			indent = "  "
		}
		got = append(got, indent+st.Name+" ("+st.Detail+") "+strings.TrimPrefix(st.Key, "c"))
	}
	want := []string{
		"Personal (Local calendar) 1",
		"Cloud (CalDAV account · 2 calendars) 2",
		"  Work (Cloud) 2:/u/a/",
		"  Home (Cloud) 2:/u/b/",
		"Work (CalDAV account · disabled) 3",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("targets:\n%s", strings.Join(got, "\n"))
	}
}
