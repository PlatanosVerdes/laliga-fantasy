# The clause window

A buyout clause cannot be paid whenever you like. The game shuts the window in the 24 hours
before a matchday starts, and the refusal on the way in is this:

```
POST /v1/competition/1/league/{league}/buyout/{slot}/pay
  400 {"errorCode":"030.01.17",
       "message":"It's not allowed to pay the buyout clause if a new fixture is starting in
                  less that one day"}
```

The message is only half the rule, and reading it alone cost a whole season of weekends (see
below). The other half is in the game's own FAQ:

> The window to purchase players by paying their release clause **closes 24 hours before a
> matchday begins and reopens once the matchday starts.**
> — [LALIGA Fantasy FAQ, Rulebook](https://laligafantasy.zendesk.com/hc/en-us/articles/360025679853-Rulebook)

So the rule is the calendar's, not the market's, and it is the same for everybody: while the
window is shut no rival can raid you either. What the clause itself is worth, and what it
becomes when a player changes hands, is in [clauses.md](clauses.md). That is worth knowing
before spending anything, because a shield bought inside the shut hours covers hours in which
nobody could raid you anyway ([shield.md](shield.md)); the hour it starts earning its price is
the first kick-off.

## When it is shut

Shut in the 24 hours before a **matchday's first kick-off**, and open from that kick-off on.
Matchday 4 is the shape of it:

| Kick-off | |
| :--- | :--- |
| vie 21:00 | the window shuts at 21:00 **thursday**, and opens again here |
| sáb 16:15, 18:30, 21:00 | open: these are the same matchday, not a new one |
| dom 16:15, 18:30, 21:00 | open |
| lun 19:00, 21:30 | open |
| next matchday, fri 21:00 | shuts again at 21:00 thursday |

Only the first kick-off of a matchday counts, which is why the fixtures reach `schedule.Clauses`
carrying their matchday. Reading every kick-off as one of these instead was the bug: no two
kick-offs inside a matchday are a day apart, so the window came out shut from Thursday night to
Monday night, every weekend, and the panel spent them saying no clausulazo was possible while
the league was clausulazoing away. A fixture with no matchday on it counts as a matchday of its
own, which errs towards a window that shuts too often rather than one that promises a payment
the game refuses.

## Where it is computed

`schedule.Clauses(fixtures, now)` in [internal/schedule/window.go](../internal/schedule/window.go),
from the whole known calendar, past fixtures included: a matchday already under way is
recognised by its first kick-off, which is behind us. It answers three things: whether it is
open, the kick-off that keeps it shut, and the instant it opens or shuts next. Any of them can
be empty, and empty means nobody knows — with no calendar at all there is no opinion to give.

Three places read it:

- **The guard**, before sending anything: `pay_clause` is refused with the hour it reopens
  instead of arriving from the API in English and without one. An unknown window is no
  opinion — refusing on it would stop every payment there is.
- **`policies.RaidPlan`**, so a scheduled raid *waits* instead of firing into the same refusal
  every two minutes. It used to be reported as a failed action; it is a wait — and it is a wait
  of hours now, not of a weekend.
- **The two clause sections of the page**, as a line with a live countdown.

`030.01.17` is still translated in `writes.explain`, for the case where nothing has told the
guard the calendar.
