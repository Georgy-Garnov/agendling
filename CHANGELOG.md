# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Calendar zoom from 80% to 160% on Windows and Linux, with a reset button and a saved zoom level.

## [0.3.2] - 2026-11-08

### Fixed
- Linux: clicking the tray icon toggles the window (hides it when shown); before it could only
  show it, e.g. on KDE Plasma (#2).
- Linux: the window keeps its position when hidden to the tray and shown again (X11; on Wayland
  the compositor places windows, a KWin window rule can remember the position).

## [0.3.1] - 2026-11-08

### Fixed
- Time zone definitions (VTIMEZONE) written to CalDAV servers carried `VALUE=TEXT` on
  `TZOFFSETFROM` / `TZOFFSETTO`, which RFC 5545 does not allow. Strict clients such as vobject and
  Home Assistant's calendar rejected the events. Events created or edited from now on are
  standard-compliant; existing ones are fixed the next time they are saved in Agendling (#3, thanks
  [@ciesinsk](https://github.com/ciesinsk)).

## [0.3.0] - 2026-11-03

### Added
- Debian package (`agendling_<version>_amd64.deb`) for Debian, Ubuntu and derivatives, with an
  applications-menu entry and icon (#2). Built with `make deb`.
- Release and CI builds fail if the Linux binary would need a glibc newer than Ubuntu 22.04's.
- [iCloud setup guide](docs/icloud.md): creating an app-specific password and adding the account.

## [0.2.1] - 2026-11-03

### Fixed
- iCloud calendars showed no events (#1): the calendar home set announced on an account partition
  host (`pNN-caldav.icloud.com`) is now followed, and events are requested with a plain
  `<calendar-data/>`, which iCloud answers with complete objects. Credentials are only sent to
  the new host over HTTPS within the same parent domain.
- The event query time range is sent in UTC; it was shifted by the local UTC offset.
- Each sync logs how many calendars and events it fetched, and unparsable objects are skipped
  with a log line instead of failing the sync.
- "Sync now" in Settings did not sync an account added (or edited) in the same dialog, so its
  last-sync time never appeared; the account list now also refreshes again after 10 seconds.

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

[Unreleased]: https://github.com/Georgy-Garnov/agendling/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/Georgy-Garnov/agendling/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/Georgy-Garnov/agendling/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Georgy-Garnov/agendling/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/Georgy-Garnov/agendling/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/Georgy-Garnov/agendling/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Georgy-Garnov/agendling/releases/tag/v0.1.0
