// Package ics converts between iCalendar objects (RFC 5545) and model events.
package ics

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"

	"github.com/Georgy-Garnov/agendling/internal/model"
)

const (
	dateFormat     = "20060102"
	datetimeFormat = "20060102T150405"
	prodID         = "-//Agendling//Agendling//EN"
)

// ToEvents extracts all VEVENTs (master and overrides) from a calendar object.
func ToEvents(cal *ical.Calendar, href, etag string) []*model.Event {
	var res []*model.Event
	for _, comp := range cal.Children {
		if comp.Name != ical.CompEvent {
			continue
		}
		e, err := toEvent(comp)
		if err != nil {
			continue // skip malformed components rather than failing the whole sync
		}
		e.Href = href
		e.ETag = etag
		res = append(res, e)
	}
	return res
}

func toEvent(comp *ical.Component) (*model.Event, error) {
	p := comp.Props
	e := &model.Event{
		UID:         text(p, ical.PropUID),
		Title:       text(p, ical.PropSummary),
		Description: text(p, ical.PropDescription),
		Location:    text(p, ical.PropLocation),
	}
	if u := p.Get(ical.PropURL); u != nil {
		e.URL = u.Value
	}
	if e.UID == "" {
		return nil, fmt.Errorf("VEVENT without UID")
	}

	dtstart := p.Get(ical.PropDateTimeStart)
	if dtstart == nil {
		return nil, fmt.Errorf("VEVENT %s without DTSTART", e.UID)
	}
	start, allDay, tzid, err := parseTimeZone(dtstart)
	if err != nil {
		return nil, err
	}
	e.StartTime, e.AllDay, e.TZID = start, allDay, tzid

	switch {
	case p.Get(ical.PropDateTimeEnd) != nil:
		end, _, err := ParseTime(p.Get(ical.PropDateTimeEnd))
		if err != nil {
			return nil, err
		}
		e.EndTime = end
	case p.Get(ical.PropDuration) != nil:
		d, err := p.Get(ical.PropDuration).Duration()
		if err != nil {
			return nil, err
		}
		e.EndTime = start.Add(d)
	case allDay:
		e.EndTime = start.AddDate(0, 0, 1)
	default:
		e.EndTime = start
	}
	if e.EndTime.Before(e.StartTime) {
		e.EndTime = e.StartTime
	}

	if r := p.Get(ical.PropRecurrenceRule); r != nil {
		e.RRule = r.Value
	}
	for _, ex := range p.Values(ical.PropExceptionDates) {
		e.ExDates = append(e.ExDates, parseTimeList(&ex)...)
	}
	if rid := p.Get(ical.PropRecurrenceID); rid != nil {
		if t, _, err := ParseTime(rid); err == nil {
			e.RecurrenceID = t
		}
	}

	for _, child := range comp.Children {
		if child.Name != ical.CompAlarm {
			continue
		}
		if off, ok := alarmOffset(child, e.StartTime, e.EndTime); ok {
			e.Reminders = append(e.Reminders, off)
		}
	}
	return e, nil
}

func text(p ical.Props, name string) string {
	s, err := p.Text(name)
	if err != nil {
		if prop := p.Get(name); prop != nil {
			return prop.Value
		}
	}
	return s
}

// alarmOffset converts a VALARM TRIGGER into "time before start".
func alarmOffset(alarm *ical.Component, start, end time.Time) (model.ReminderOffset, bool) {
	trig := alarm.Props.Get(ical.PropTrigger)
	if trig == nil {
		return 0, false
	}
	if trig.ValueType() == ical.ValueDateTime || (len(trig.Value) > 0 && trig.Value[0] != '-' && trig.Value[0] != '+' && trig.Value[0] != 'P') {
		t, err := trig.DateTime(time.UTC)
		if err != nil {
			return 0, false
		}
		return model.ReminderOffset(start.Sub(t)), true
	}
	d, err := trig.Duration()
	if err != nil {
		return 0, false
	}
	if strings.EqualFold(trig.Params.Get(ical.ParamRelated), "END") {
		d += end.Sub(start)
	}
	if d > 0 {
		return 0, false // reminders after start are not supported
	}
	return model.ReminderOffset(-d), true
}

// ParseTime parses DTSTART-like properties, tolerating unknown TZIDs.
func ParseTime(prop *ical.Prop) (t time.Time, allDay bool, err error) {
	t, allDay, _, err = parseTimeZone(prop)
	return t, allDay, err
}

// parseTimeZone also reports the IANA zone the value is expressed in:
// "" for floating/all-day values, "UTC" for Z-suffixed ones.
func parseTimeZone(prop *ical.Prop) (t time.Time, allDay bool, tzid string, err error) {
	v := strings.TrimSpace(prop.Value)
	if prop.ValueType() == ical.ValueDate || len(v) == len(dateFormat) {
		t, err = time.ParseInLocation(dateFormat, v, time.Local)
		return t, true, "", err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.Parse(datetimeFormat+"Z", v)
		return t.Local(), false, "UTC", err
	}
	loc := time.Local
	if id := prop.Params.Get(ical.PropTimezoneID); id != "" {
		loc = resolveZone(id)
		if loc != time.Local {
			tzid = loc.String()
		}
	}
	t, err = time.ParseInLocation(datetimeFormat, v, loc)
	return t.Local(), false, tzid, err
}

func parseTimeList(prop *ical.Prop) []time.Time {
	var res []time.Time
	for _, v := range strings.Split(prop.Value, ",") {
		single := *prop
		single.Value = strings.TrimSpace(v)
		if t, _, err := ParseTime(&single); err == nil {
			res = append(res, t)
		}
	}
	return res
}

// windowsZones maps the Windows zone names that Outlook/Exchange put into TZID to IANA.
var windowsZones = map[string]string{
	"Russian Standard Time":          "Europe/Moscow",
	"Ekaterinburg Standard Time":     "Asia/Yekaterinburg",
	"N. Central Asia Standard Time":  "Asia/Novosibirsk",
	"North Asia Standard Time":       "Asia/Krasnoyarsk",
	"Kaliningrad Standard Time":      "Europe/Kaliningrad",
	"Samara Standard Time":           "Europe/Samara",
	"W. Europe Standard Time":        "Europe/Berlin",
	"Central Europe Standard Time":   "Europe/Budapest",
	"Romance Standard Time":          "Europe/Paris",
	"GMT Standard Time":              "Europe/London",
	"FLE Standard Time":              "Europe/Kyiv",
	"GTB Standard Time":              "Europe/Bucharest",
	"Belarus Standard Time":          "Europe/Minsk",
	"Turkey Standard Time":           "Europe/Istanbul",
	"Israel Standard Time":           "Asia/Jerusalem",
	"Georgian Standard Time":         "Asia/Tbilisi",
	"Caucasus Standard Time":         "Asia/Yerevan",
	"Azerbaijan Standard Time":       "Asia/Baku",
	"Central Asia Standard Time":     "Asia/Almaty",
	"West Asia Standard Time":        "Asia/Tashkent",
	"Omsk Standard Time":             "Asia/Omsk",
	"North Asia East Standard Time":  "Asia/Irkutsk",
	"Yakutsk Standard Time":          "Asia/Yakutsk",
	"Vladivostok Standard Time":      "Asia/Vladivostok",
	"Magadan Standard Time":          "Asia/Magadan",
	"Russia Time Zone 3":             "Europe/Samara",
	"Russia Time Zone 10":            "Asia/Srednekolymsk",
	"Russia Time Zone 11":            "Asia/Kamchatka",
	"Astrakhan Standard Time":        "Europe/Astrakhan",
	"Volgograd Standard Time":        "Europe/Volgograd",
	"Saratov Standard Time":          "Europe/Saratov",
	"Altai Standard Time":            "Asia/Barnaul",
	"Tomsk Standard Time":            "Asia/Tomsk",
	"Arabian Standard Time":          "Asia/Dubai",
	"Singapore Standard Time":        "Asia/Singapore",
	"Korea Standard Time":            "Asia/Seoul",
	"AUS Eastern Standard Time":      "Australia/Sydney",
	"Central European Standard Time": "Europe/Warsaw",
	"Greenwich Standard Time":        "Atlantic/Reykjavik",
	"E. Europe Standard Time":        "Europe/Chisinau",
	"Eastern Standard Time":          "America/New_York",
	"Central Standard Time":          "America/Chicago",
	"Mountain Standard Time":         "America/Denver",
	"Pacific Standard Time":          "America/Los_Angeles",
	"China Standard Time":            "Asia/Shanghai",
	"Tokyo Standard Time":            "Asia/Tokyo",
	"India Standard Time":            "Asia/Kolkata",
	"UTC":                            "UTC",
}

// WindowsZoneToIANA maps a Windows time zone key (e.g. "FLE Standard Time") to its IANA name.
func WindowsZoneToIANA(key string) (string, bool) {
	iana, ok := windowsZones[key]
	return iana, ok
}

func resolveZone(tzid string) *time.Location {
	tzid = strings.Trim(tzid, "\"")
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc
	}
	if iana, ok := windowsZones[tzid]; ok {
		if loc, err := time.LoadLocation(iana); err == nil {
			return loc
		}
	}
	// Some producers use "/mozilla.org/.../Europe/Berlin"-style ids: try the tail.
	if i := strings.Index(tzid, "/"); i >= 0 {
		parts := strings.Split(tzid, "/")
		for k := 0; k < len(parts)-1; k++ {
			if loc, err := time.LoadLocation(strings.Join(parts[k:], "/")); err == nil {
				return loc
			}
		}
	}
	return time.Local
}

// NewUID generates a globally unique iCalendar UID.
func NewUID() string { return uuid.NewString() + "@agendling" }

// FromEvents builds a calendar object containing a master event and its overrides.
// All events must share one UID.
func FromEvents(events []*model.Event) *ical.Calendar {
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, prodID)
	now := time.Now().UTC()
	zones := map[string]bool{}
	for _, e := range events {
		if loc := writeZone(e); loc != nil && !zones[loc.String()] {
			zones[loc.String()] = true
			cal.Children = append(cal.Children, VTimezone(loc, e.StartTime))
		}
	}
	for _, e := range events {
		cal.Children = append(cal.Children, fromEvent(e, now))
	}
	return cal
}

func fromEvent(e *model.Event, now time.Time) *ical.Component {
	ev := ical.NewEvent()
	p := ev.Props
	p.SetText(ical.PropUID, e.UID)
	p.SetDateTime(ical.PropDateTimeStamp, now)
	zone := writeZone(e)
	setTime(p, ical.PropDateTimeStart, e.StartTime, e.AllDay, zone)
	end := e.EndTime
	if e.AllDay && !end.After(e.StartTime) {
		end = e.StartTime.AddDate(0, 0, 1)
	}
	setTime(p, ical.PropDateTimeEnd, end, e.AllDay, zone)
	p.SetText(ical.PropSummary, e.Title)
	if e.Description != "" {
		p.SetText(ical.PropDescription, e.Description)
	}
	if e.Location != "" {
		p.SetText(ical.PropLocation, e.Location)
	}
	if e.URL != "" {
		u := ical.NewProp(ical.PropURL)
		u.SetValueType(ical.ValueURI)
		u.Value = e.URL
		p.Set(u)
	}
	if e.RRule != "" && e.RecurrenceID.IsZero() {
		r := ical.NewProp(ical.PropRecurrenceRule)
		r.SetValueType(ical.ValueRecurrence)
		r.Value = strings.TrimPrefix(e.RRule, "RRULE:")
		p.Set(r)
		for _, ex := range e.ExDates {
			prop := ical.NewProp(ical.PropExceptionDates)
			setPropTime(prop, ex, e.AllDay, zone)
			p.Add(prop)
		}
	}
	if !e.RecurrenceID.IsZero() {
		setTime(p, ical.PropRecurrenceID, e.RecurrenceID, e.AllDay, zone)
	}
	for _, r := range e.Reminders {
		alarm := ical.NewComponent(ical.CompAlarm)
		alarm.Props.SetText(ical.PropAction, "DISPLAY")
		alarm.Props.SetText(ical.PropDescription, e.Title)
		trig := ical.NewProp(ical.PropTrigger)
		trig.SetDuration(-time.Duration(r))
		alarm.Props.Set(trig)
		ev.Children = append(ev.Children, alarm)
	}
	return ev.Component
}

// writeZone returns the zone to serialize an event's times in, or nil for UTC/all-day.
func writeZone(e *model.Event) *time.Location {
	if e.AllDay || e.TZID == "" || e.TZID == "UTC" {
		return nil
	}
	if loc := e.Zone(); loc != time.Local {
		return loc
	}
	return nil
}

func setTime(p ical.Props, name string, t time.Time, allDay bool, zone *time.Location) {
	prop := ical.NewProp(name)
	setPropTime(prop, t, allDay, zone)
	p.Set(prop)
}

// setPropTime writes a date, a zoned local time (TZID + matching VTIMEZONE), or UTC.
// Recurring events need the zone so that repeats keep their wall-clock time across DST.
func setPropTime(prop *ical.Prop, t time.Time, allDay bool, zone *time.Location) {
	switch {
	case allDay:
		prop.SetDate(t)
	case zone != nil:
		prop.SetValueType(ical.ValueDateTime)
		prop.Params.Set(ical.PropTimezoneID, zone.String())
		prop.Value = t.In(zone).Format(datetimeFormat)
	default:
		prop.SetDateTime(t.UTC())
	}
}
