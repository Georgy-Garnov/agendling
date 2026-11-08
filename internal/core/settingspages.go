package core

import (
	"strings"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

// SettingsPage is a section of the Settings dialog: listed on the left, shown on the right.
type SettingsPage struct {
	Key      string
	Title    string
	Intro    string // one line under the page title
	Icon     string // freedesktop icon name (GTK)
	Keywords string // setting names and synonyms the search box matches
}

// SettingsPages lists the Settings dialog sections in display order.
var SettingsPages = []SettingsPage{
	{Key: "calendars", Title: "Calendars", Icon: "x-office-calendar-symbolic",
		Intro:    "CalDAV accounts and local calendars, and which of their calendars to sync.",
		Keywords: "account caldav server url username password app-specific icloud apple nextcloud fastmail radicale local add edit remove delete sync now color colour interval status"},
	{Key: "reminders", Title: "Reminders", Icon: "alarm-symbolic",
		Intro:    "When Agendling reminds you of upcoming events.",
		Keywords: "default reminders minutes hours before snooze interval alert popup notification"},
	{Key: "sound", Title: "Sound", Icon: "audio-volume-high-symbolic",
		Intro:    "The sound played with reminders and new-meeting alerts.",
		Keywords: "audio chime melody mp3 wav file browse test play stop repeat loop until stopped mute"},
	{Key: "alerts", Title: "New meetings", Icon: "mail-unread-symbolic",
		Intro:    "Alerts for events that appear in your calendars after a sync.",
		Keywords: "new event meeting invitation alert notify popup sound sync"},
	{Key: "startup", Title: "Startup", Icon: "system-run-symbolic",
		Intro:    "How Agendling starts together with your computer.",
		Keywords: "run at login windows startup autostart start minimized tray background close quit exit"},
	{Key: "about", Title: "About", Icon: "help-about-symbolic",
		Intro:    "Version, updates and where your data is stored.",
		Keywords: "version update check for updates new release github download license data folder log file privacy"},
}

// FilterSettingsPages returns the pages whose title, intro or keywords contain every word of
// query (case-insensitive); an empty query returns all pages.
func FilterSettingsPages(query string) []SettingsPage {
	words := strings.Fields(strings.ToLower(query))
	var res []SettingsPage
	for _, p := range SettingsPages {
		text := strings.ToLower(p.Title + " " + p.Intro + " " + p.Keywords)
		match := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				match = false
				break
			}
		}
		if match {
			res = append(res, p)
		}
	}
	return res
}

// SameEditableSettings compares the settings edited in the Settings dialog, ignoring those
// changed elsewhere (calendar ticks, the calendar list, mute).
func SameEditableSettings(a, b model.AppSettings) bool {
	return FormatReminders(a.DefaultReminderMinutes) == FormatReminders(b.DefaultReminderMinutes) &&
		a.SnoozeIntervalMinutes == b.SnoozeIntervalMinutes && a.CustomSoundPath == b.CustomSoundPath &&
		a.LoopSound == b.LoopSound && a.NewEventAlertsOff == b.NewEventAlertsOff &&
		a.RunAtStartup == b.RunAtStartup && a.StartMinimized == b.StartMinimized &&
		a.CloseWithoutTray == b.CloseWithoutTray
}
