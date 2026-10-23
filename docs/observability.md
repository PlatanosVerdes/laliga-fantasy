# Measuring the page and logging

## Measuring the page

A layout argued from memory is an argument nobody can win, so the page records what it is used
for and the answer is a table.

Half of it needed no code. Every page load, every player card, every operation is a request, so
the reverse proxy's access log already held twenty-eight days of it: 605 loads, 490 cards over
135 different players, 92 bids started against 76 confirmed, and thirteen scheduled raids against
nine cancelled by hand, which is what sent somebody to look at
[the raid that outlived its target](refreshing.md#paying-a-clause-re-reads-it-first).

The other half never leaves the browser. Which tab is open, what is pressed inside it, which
column a table is sorted by: with nine tabs and thirty sections that is the half that decides
where things go, and no request carries it. So the page sends it, batched every fifteen seconds
and on the way out, to `/api/usage`, which appends it to `usage.log` in the state directory.

Three rules it follows, because each of them is a way the measurement would lie:

* **Time runs only while the tab is visible.** A fantasy panel sits open in a window all
  afternoon. Without `visibilitychange` the winner of "where do you spend your time" is whichever
  tab happened to be open when you walked away.
* **A click is filed under its section, not its label.** The same "Pujar" from four tables is one
  row that says nothing about where to put it.
* **The static report records nothing.** It has nowhere to send it, and a fetch that always fails
  is noise in somebody's console rather than a measurement. The page knows by the `data-mode` the
  server stamps in the header.

`fantasy usage --days 7` prints it from the file. Every event also goes to the log, because the
file sits in the container's volume where nothing collecting logs off the host can reach it:
`container:laliga-fantasy AND _msg:usage` is the deployed copy.

Nothing is sent anywhere else. A hosted analytics tool would have meant shipping a page carrying
your balance, your league and buttons that spend money to somebody else's server, and the only
thing protecting it is that nobody else can reach it. For one user a heat map is the wrong
instrument anyway: it is a device for averaging thousands of sessions, and with ten actions a
session the sorted list *is* the heat map.

## Logging

Human-readable lines on stderr (warnings only, `-v` for everything) and JSON lines in
`fantasy.log` in the state directory, rotated at 5 MB × 3. Each record carries `service`, `level`, `host` and
the event's own fields (`url`, `status`, `ms`, `matched`, ...).

Set `FANTASY_LOG_JSON=1` to emit JSON on **stdout** instead of the terminal format — that is
what the container image does, so Vector's docker source collects it with no configuration.

Set `FANTASY_LOG_URL` to also push records straight into VictoriaLogs:

```bash
FANTASY_LOG_URL='http://<pi>:9428/insert/jsonline?_stream_fields=service' ./fantasy advise
```

Pushes are fire-and-forget on a daemon thread, so an unreachable sink never blocks or fails
a command. Alternatively, if the repo lives on the homeserver, Vector already tails `*.log`
from repo roots into VictoriaLogs and needs no configuration.
