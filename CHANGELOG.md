# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- First public version of Agendling for Windows (native Win32 UI) and Linux (GTK 3).
- Day, Week, 2-week and Month views with colour-coded calendars.
- Two-way CalDAV sync with several accounts, plus local calendars.
- Event editor with all-day events, repeats, reminders and clickable links from the description.
- Always-on-top reminder popups with a custom sound, instant Stop Sound, Snooze and Dismiss.
- New-meeting alerts when a sync brings in a previously unknown upcoming event.
- System tray with Open, Sync Now, Mute Notifications, Settings and Exit; closes to the tray.
- Single-instance behaviour: launching again brings the running window to the front.
- Time-zone-correct recurrence (repeats in the event's own TZID) and VTIMEZONE generation.
- Encrypted password storage (Windows DPAPI; AES-GCM with the desktop keyring on Linux).
- Run at startup / start minimized.
