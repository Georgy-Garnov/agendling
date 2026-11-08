# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Calendar zoom from 80% to 160% on Windows and Linux, with a reset button and a saved zoom level.
- **Maximum sound length** in Settings → Sound: how long an alert sound may play, repeated or not
  (3 minutes by default, e.g. 15 seconds, or *No limit* to play until stopped). A long sound file
  played once now stops there too (#8, thanks [@ciesinsk](https://github.com/ciesinsk)).
- **Sounds per kind of alert and per calendar:** meeting reminders and new meetings can each have a
  sound of their own, and so can every account, local calendar and calendar of an account. A
  calendar's sound comes first, then its account's, then the kind of alert, then the main alert
  sound, so existing setups keep sounding as before.

### Changed
- *Repeat until stopped* repeats up to the maximum length instead of a fixed 3 minutes; ▶ Test
  in Settings still plays the whole file.

### Fixed
- An event whose title, description or location contains a carriage return (as some servers send
  it) no longer fails to save or sync: line breaks of any kind are now written correctly.
- Event titles with line breaks (common in synced invitations) are shown on one line in the
  calendar grid instead of being clipped (#7, thanks [@ciesinsk](https://github.com/ciesinsk)).

## [0.4.0] - 2026-11-08

### Added
- **Choose which calendars to show:** the **Calendars ▾** toolbar button opens a list of every
  calendar, including each calendar of a CalDAV account (e.g. all iCloud calendars), with its
  colour, account and an eye to show or hide it; pick one or several. Hidden calendars keep syncing
  and reminding but are not drawn and stay quiet on new meetings.
- **Pick the calendar inside an account** in the event editor, and move events between calendars
  of the same account (stored in the new calendar, then removed from the old one on the server).
- **Choose which calendars of an account to sync** in its Edit dialog after *Test connection*;
  calendars created on the server later are synced by default.
- **Redesigned Settings:** sections on the left (Calendars, Reminders, Sound, New meetings,
  Startup, About), the settings on the right, a search box that finds a section by any setting
  name, and OK / Cancel / Apply.
- **About** page with the version, the data folder and a **Check for updates** button that asks
  GitHub for the latest release on request and links to its release page (no automatic checks, no
  downloads).
- **Refreshed look:** a new toolbar (view switcher, calendar dropdown, accent *New event*), and the
  account and event editors in the same card style as Settings, on Windows and Linux.
- Reminder and new-meeting popups in the same style, naming the exact calendar (e.g. *iCloud / Family*).
- Linux: a **☰** menu in the header with Settings, About and **Quit** (Ctrl+Q). Without a system
  tray, closing the window asks once whether to keep running minimized or to quit (Settings →
  Startup); tray detection now also checks that something actually displays tray icons.
- About shows the app logo; an update check that fails (e.g. GitHub's timeouts for anonymous API
  requests) is retried up to three times, explained, and links to the list of releases.
- Windows: Settings, account and event dialogs show the app icon.
- `make uishots` builds a screenshot tour of the Windows UI with demo data, for reviewing the look.

### Fixed
- Linux: *Edit…* in Settings → Calendars crashed the app.
- Windows: OK in Settings (and Save/OK in the event and account editors) could close the whole app:
  the dialog was closed while walk still had a layout pending for it.
- A crash now leaves its stack trace in `agendling.log` instead of vanishing silently.

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

[Unreleased]: https://github.com/Georgy-Garnov/agendling/compare/v0.4.0...HEAD
[0.4.0]: https://github.com/Georgy-Garnov/agendling/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/Georgy-Garnov/agendling/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/Georgy-Garnov/agendling/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/Georgy-Garnov/agendling/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/Georgy-Garnov/agendling/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/Georgy-Garnov/agendling/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/Georgy-Garnov/agendling/releases/tag/v0.1.0
