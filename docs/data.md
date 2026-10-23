# Where things live

Files follow the [XDG base directory spec](https://specifications.freedesktop.org/basedir-spec/latest/),
split by what they are — so a backup of the first directory is a backup of everything that
cannot be regenerated:

| Directory | Default | Holds |
|---|---|---|
| config | `~/.config/laliga-fantasy/` | `active.json`, `secret.key`, each account's sealed `tokens.json` (`0600`), each league's `settings.json` and `policies.json` |
| state | `~/.local/state/laliga-fantasy/` | each league's `report.html` and history, the shared `fantasy.log` |
| cache | `~/.cache/laliga-fantasy/` | scraped futbolfantasy pages, 24 h TTL, and `ff_ceilings.json`, every player page read with when (ceiling, season high and low, open injury); safe to delete |

## Accounts and leagues

Several LaLiga accounts can be saved, one of them active at a time, each with its own leagues.
The session belongs to the account; settings, standing instructions, house rules, history and
cache belong to the league, because an instruction in one league means nothing in another:

```
<config>/active.json                          which account and league are in use
<config>/accounts/<account>/tokens.json       the account's session
<config>/accounts/<account>/leagues/<id>/     the league's settings and instructions
<state>/accounts/<account>/leagues/<id>/      its history
<cache>/accounts/<account>/leagues/<id>/      its cache
<cache>/global/                               what is the same for every league: the player
                                              catalogue, the calendar, futbolfantasy's pages
```

The account menu at the top right switches league or account, adds an account and logs out.
A switch writes `active.json` and restarts the process in place, so no path ever changes under
a reader; the page shows the build's progress until the new league is ready. An account in a
single league gets it chosen automatically; with several, the page asks. Logging out deletes
only that account's session; its leagues' instructions stay for the next login.

**Signing in.** The LaLiga login is the panel's login: every browser signs in once with the
paste flow and gets a server-side session (an `HttpOnly`, `Secure`, `SameSite=Lax` cookie, 30
days idle, 90 at most; only `localhost` gets it over plain HTTP). A session sees only the
accounts of the LaLiga user it signed in as; when the panel is following somebody else's, the
page says so and offers to switch to one of yours. "Cerrar sesión" ends the session here or on every device
and leaves the account's tokens, and so the automatic engine, alone. Who may sign in is
**`FANTASY_ALLOW`**: LaLiga user ids or emails separated by commas, or `*`. Unset, the users of
the accounts already saved may, and on a fresh install the first login becomes the owner.

An install from before profiles is moved into its first profile on the first start, once.
Back up the data directory before upgrading.

To watch two leagues **at the same time**, run a second instance with its own
`FANTASY_DATA_DIR` and `--port`: one process works on one profile.

`XDG_CONFIG_HOME`, `XDG_STATE_HOME` and `XDG_CACHE_HOME` are honoured. Two overrides:

* **`FANTASY_DATA_DIR`** collapses all three into one directory. That is what a container
  wants — a single volume to mount — and it is what the image sets.
* If a `data/` directory next to the code already has a session in it, it keeps being used, and
  the first run moves those files to the locations above and tells you so. Upgrading never
  costs a login.

**Seeding a container without an interactive login.** The refresh token *rotates*: the API
can hand back a new one on every renewal, so it has to be written somewhere — which is why
`gh`, `aws` and `kubectl` all keep rotating credentials in a file rather than an environment
variable. To hand over the first one, set **`FANTASY_TOKENS`** (the whole `tokens.json` as
JSON) or **`FANTASY_REFRESH_TOKEN`** (just the refresh token, exchanged on startup). Either
one is read only when no token file exists, is written to the file at `0600`, and rotation
takes over from there — so you can drop the variable after the first start.
