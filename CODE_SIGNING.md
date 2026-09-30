# Code signing policy

Free code signing provided by [SignPath.io](https://about.signpath.io), certificate by
[SignPath Foundation](https://signpath.org).

## What is signed

Only the Windows executable `agendling.exe` published on the
[Releases](https://github.com/Georgy-Garnov/agendling/releases) page is signed. It is built from
this repository's source code by the [release workflow](.github/workflows/release.yml) on GitHub
Actions and is submitted to SignPath from that workflow; binaries built anywhere else are never
signed. No third-party or upstream binaries are signed.

Each signing request is approved manually by a project approver before the signed file is
published.

## Team roles

| Role | Members |
|---|---|
| Committers and reviewers | [Georgiy Garnov (@Georgy-Garnov)](https://github.com/Georgy-Garnov) |
| Approvers | [Georgiy Garnov (@Georgy-Garnov)](https://github.com/Georgy-Garnov) |

Committers may change the source code; changes from outside contributors are reviewed by a
reviewer before they are merged. Approvers authorise each code signing request. All members use
multi-factor authentication on GitHub and SignPath.

## Privacy policy

This program will not transfer any information to other networked systems unless specifically
requested by the user or the person installing or operating it.

Agendling connects only to the CalDAV servers that the user configures, to synchronise that user's
calendars. It contains no telemetry, analytics, advertising or update checks. Credentials are stored
encrypted on the user's computer (see [Data and privacy](README.md#data-and-privacy)).

## Uninstallation

Agendling has no installer. To remove it, exit the app from the tray menu, disable
*Run at Windows startup* in Settings, then delete `agendling.exe` and the `%AppData%\Agendling`
folder. See [Data and privacy](README.md#data-and-privacy) for details.

## Reporting a problem

If a signed Agendling binary behaves unexpectedly or you suspect it was not built from this
repository, please report it privately as described in [SECURITY.md](SECURITY.md).
