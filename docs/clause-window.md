# The clause window

A buyout clause cannot be paid whenever you like. The game refuses it while the matchday is
under way, and the refusal is this:

```
POST /v1/competition/1/league/{league}/buyout/{slot}/pay
  400 {"errorCode":"030.01.17",
       "message":"It's not allowed to pay the buyout clause if a new fixture is starting in
                  less that one day"}
```

So the rule is the calendar's, not the market's, and it is the same for everybody: while the
window is shut no rival can raid you either. That is worth knowing before spending anything,
because a shield bought inside those hours protects against nothing
([shield.md](shield.md)).

## When it is shut

Shut whenever a fixture kicks off within the next 24 hours. Matchday 4 is the shape of it:

| Kick-off | |
| :--- | :--- |
| vie 21:00 | the window shuts at 21:00 **thursday** |
| sáb 16:15, 18:30, 21:00 | |
| dom 16:15, 18:30, 21:00 | |
| lun 19:00, 21:30 | |
| next matchday, fri 21:00 | the window is open again from mon 21:30 to thu 21:00 |

No two kick-offs in a matchday are a day apart, so the whole weekend is closed in one piece.
It opens again **when the last match of the matchday starts**, not when it ends: from that
kick-off on, no new fixture begins within the day. That reading is what the rule says word for
word; if the game measures from the final whistle instead, the real reopening is a couple of
hours later than the one computed here.

## Where it is computed

`schedule.Clauses(fixtures, now)` in [internal/schedule/window.go](../internal/schedule/window.go),
from the whole known calendar rather than this matchday's — when it opens again is the next
matchday's business. It answers three things: whether it is open, the kick-off that keeps it
shut, and the instant it opens or shuts next. Any of them can be empty, and empty means nobody
knows: with the calendar ending inside the matchday there is no gap to find, and inventing an
hour would stand a raid down until a moment that does not exist.

Three places read it:

- **The guard**, before sending anything: `pay_clause` is refused with the hour it reopens
  instead of arriving from the API in English and without one. An unknown window is no
  opinion — refusing on it would stop every payment there is.
- **`policies.RaidPlan`**, so a scheduled raid *waits* instead of firing into the same refusal
  every two minutes all weekend. It used to be reported as a failed action; it is a wait.
- **The two clause sections of the page**, as a line with a live countdown.

`030.01.17` is still translated in `writes.explain`, for the case where nothing has told the
guard the calendar.
