# Contributing to Agendling

Thanks for helping! Agendling aims to be a small, quiet and dependable calendar. Please keep that in
mind when proposing features: anything that adds idle CPU work, background polling or noticeable
memory needs a strong reason.

## Reporting bugs

Open an [issue](https://github.com/Georgy-Garnov/agendling/issues/new/choose) with:

- the version (`agendling --version`) and your OS;
- the CalDAV server type, if sync is involved (never include passwords);
- steps to reproduce, and a screenshot if it's a UI problem;
- relevant lines from `agendling.log` in the data directory (see the README), with anything private removed.

## Development setup

- Go 1.26+.
- Linux: `sudo apt install pkg-config libgtk-3-dev` to build the GTK UI.
- The Windows UI cross-compiles from any OS without a C compiler: `make windows`.

```sh
make test   # unit + integration tests (no network, no display needed)
make vet    # go vet for Linux and Windows
make build  # both binaries into bin/
```

The tests include an in-process CalDAV server (`internal/caldav/sync_test.go`), so sync changes can
be tested without a real account.

## Code layout

UI-independent logic belongs in `internal/core` (or a lower package), not in `internal/ui` (Windows)
or `internal/gtkui` (Linux). Both front ends should stay feature-equivalent; if you change one,
please mention in the PR whether the other needs the same change.

## Pull requests

- Keep PRs focused; one topic per PR.
- Run `gofmt`, `make vet` and `make test` before pushing.
- Add or update tests for behaviour changes, especially in `core`, `caldav`, `ics`, `recur` and
  `scheduler`.
- For UI changes, attach a screenshot (Windows and/or Linux).
- If you add or update a dependency, run `make notices` and commit the regenerated
  `THIRD_PARTY_NOTICES.md`. Only permissively licensed dependencies (MIT, BSD, ISC, Apache-2.0 or
  similar) can be accepted.

By contributing you agree that your contributions are licensed under the project's MIT License.
