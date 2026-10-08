# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] - 2026-10-08

### Added
- Windows executable now carries an icon and version information (product name, version,
  copyright), generated from `cmd/agendling/winres/winres.json` with go-winres.
- Code signing policy (`CODE_SIGNING.md`) and release workflow support for signing the Windows
  executable through SignPath Foundation.

### Changed
- CalDAV sync is much lighter on servers: calendar discovery is cached for a day and a calendar is
  only downloaded again when its change tag (`getctag` / `sync-token`) moves, so a sync with no
  remote changes is a single small PROPFIND.
- Requests carry an `Agendling/<version>` User-Agent instead of the Go default.
- Sources on the same account no longer sync in parallel.

### Fixed
- Rate-limit responses (HTTP 429, or 503 with `Retry-After`) now pause syncing of the account for
  the time the server asks (or with exponential back-off), including across app restarts; the full
  response headers are written to the log for diagnosis.

## [0.1.0] - 2026-09-30

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

[Unreleased]: https://github.com/Georgy-Garnov/agendling/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/Georgy-Garnov/agendling/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Georgy-Garnov/agendling/releases/tag/v0.1.0
