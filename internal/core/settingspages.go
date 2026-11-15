package core

import (
	"strings"

	"github.com/Georgy-Garnov/agendling/internal/i18n"
	"github.com/Georgy-Garnov/agendling/internal/model"
)

// SettingsPage is a section of the Settings dialog: listed on the left, shown on the right.
// Title, Intro and Keywords are English; show them with i18n.T.
type SettingsPage struct {
	Key      string
	Title    string
	Intro    string // one line under the page title
	Icon     string // freedesktop icon name (GTK)
	Keywords string // setting names and synonyms the search box matches
}

// SettingsPages lists the Settings dialog sections in display order.
var SettingsPages = []SettingsPage{
	{Key: "calendars", Title: i18n.Mark("Calendars"), Icon: "x-office-calendar-symbolic",
		Intro:    i18n.Mark("CalDAV accounts and local calendars, and which of their calendars to sync."),
		Keywords: i18n.Mark("account caldav server url username password app-specific icloud apple nextcloud fastmail radicale local add edit remove delete sync now color colour interval status")},
	{Key: "reminders", Title: i18n.Mark("Reminders"), Icon: "alarm-symbolic",
		Intro:    i18n.Mark("When Agendling reminds you of upcoming events."),
		Keywords: i18n.Mark("default reminders minutes hours before snooze interval alert popup notification")},
	{Key: "sound", Title: i18n.Mark("Sound"), Icon: "audio-volume-high-symbolic",
		Intro:    i18n.Mark("The sounds played with reminders and new-meeting alerts."),
		Keywords: i18n.Mark("audio chime melody mp3 wav file browse test play stop repeat loop until stopped mute maximum length duration limit minutes seconds no limit own custom sound per calendar account meeting reminder new meeting invitation")},
	{Key: "alerts", Title: i18n.Mark("New meetings"), Icon: "mail-unread-symbolic",
		Intro:    i18n.Mark("Alerts for events that appear in your calendars after a sync."),
		Keywords: i18n.Mark("new event meeting invitation alert notify popup sound sync")},
	{Key: "startup", Title: i18n.Mark("Startup"), Icon: "system-run-symbolic",
		Intro:    i18n.Mark("How Agendling starts together with your computer."),
		Keywords: i18n.Mark("run at login windows startup autostart start minimized tray background close quit exit")},
	{Key: "about", Title: i18n.Mark("About"), Icon: "help-about-symbolic",
		Intro:    i18n.Mark("Version, updates and where your data is stored."),
		Keywords: i18n.Mark("version update check for updates new release github download license data folder log file privacy")},
}

// FilterSettingsPages returns the pages whose title, intro or keywords contain every word of
// query (case-insensitive); an empty query returns all pages. Both the English text and its
// translation are searched, so English setting names keep working in any language.
func FilterSettingsPages(query string) []SettingsPage {
	words := strings.Fields(strings.ToLower(query))
	var res []SettingsPage
	for _, p := range SettingsPages {
		text := strings.ToLower(p.Title + " " + p.Intro + " " + p.Keywords + " " +
			i18n.T(p.Title) + " " + i18n.T(p.Intro) + " " + i18n.T(p.Keywords))
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
		a.LoopSound == b.LoopSound && a.SoundLimitSeconds == b.SoundLimitSeconds && a.TypeSounds == b.TypeSounds &&
		sameCalendarSounds(a.CalendarSounds, b.CalendarSounds) && a.NewEventAlertsOff == b.NewEventAlertsOff &&
		a.RunAtStartup == b.RunAtStartup && a.StartMinimized == b.StartMinimized &&
		a.CloseWithoutTray == b.CloseWithoutTray && a.Language == b.Language
}
