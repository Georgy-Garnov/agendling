# Connecting an iCloud calendar

Agendling syncs iCloud calendars over CalDAV. Apple does not let third-party apps sign in with
your Apple ID password, so you create an **app-specific password** once and give that to
Agendling.

| Setting | Value |
|---|---|
| Server URL | `https://caldav.icloud.com` |
| Username | Your Apple ID (the email address you sign in to iCloud with) |
| Password | An app-specific password, e.g. `abcd-efgh-ijkl-mnop` |

## 1. Turn on two-factor authentication

App-specific passwords are only available for Apple IDs with two-factor authentication. Most
accounts already have it. To check on an iPhone or iPad, open **Settings → [your name] → Sign-In &
Security** and make sure **Two-Factor Authentication** is on. On a Mac it is in **System
Settings → [your name] → Sign-In & Security**.

## 2. Create an app-specific password

You can do this from any browser, including Safari on an iPhone or iPad:

1. Go to [account.apple.com](https://account.apple.com) (formerly appleid.apple.com) and sign in
   with your Apple ID. Confirm the sign-in with the code sent to one of your devices.
2. Open **Sign-In and Security → App-Specific Passwords**.
3. Click **Generate an app-specific password** (or **+**) and enter a label such as
   `Agendling`.
4. Apple shows a password in the form `abcd-efgh-ijkl-mnop`. Copy it now: it is shown only once.
   If you lose it, revoke it and create a new one.

## 3. Add the account in Agendling

1. Open **Settings → Calendars → Add CalDAV account…**
2. Fill in:
   - **Name:** anything, e.g. `iCloud`
   - **Server URL:** `https://caldav.icloud.com`
   - **Username:** your Apple ID email
   - **Password:** the app-specific password, including the dashes
3. Click **Test connection**. It should report how many calendars were found.
4. Pick a colour and a sync interval and click **OK**, or press **Sync now** right away.

Always enter `https://caldav.icloud.com`. iCloud keeps every account on its own server
(`pNN-caldav.icloud.com`) and announces it during discovery; Agendling follows it automatically
and remembers it.

## Good to know

- All event calendars of the account are shown together; new events created in Agendling go into
  the first one. Reminders lists (tasks) are not shown.
- Events from 6 months back to 2 years ahead are synced, so older one-off events do not appear.
- The first sync of a new account imports its events silently; new-meeting alerts start with the
  following syncs.
- Changing your Apple ID password revokes all app-specific passwords. Create a new one and update
  it in **Settings → Calendars → Edit**.
- You can revoke the password at any time under **App-Specific Passwords** on
  [account.apple.com](https://account.apple.com); Agendling then stops syncing that account.

## Troubleshooting

- **"401 Unauthorized"** on *Test connection*: the password is wrong or is your regular Apple ID
  password. Use an app-specific password and your full Apple ID email as the username.
- **Connected, but no events appear:** check the log file, `%AppData%\Agendling\agendling.log` on
  Windows or `~/.config/Agendling/agendling.log` on Linux. After a sync it contains lines like

  ```text
  sync "iCloud": discovered 4 calendar(s) on https://p68-caldav.icloud.com:443/
  sync "iCloud": fetched 12 event(s) from 4 of 4 calendar(s)
  ```

  If the counts look wrong, please [open an issue](https://github.com/Georgy-Garnov/agendling/issues)
  and attach these lines (they contain no passwords).
