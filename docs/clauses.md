# What a clause is, measured

The buyout clause is the standing offer every rival can accept the moment the window opens
([clause-window.md](clause-window.md)), so three numbers decide everything about it: what it
becomes when a player changes hands, how long it cannot be paid, and what raising it costs.

None of that is documented by the game, so it was measured against this league's own transfer
log on 2026-09-07: nine signings from the last four days, each one's price read from the feed
and each one's clause read from the squad payload.

## The clause a signing arrives with

```
clause = max(price paid, market value), never under 1.000.000
```

| Player | Paid | Value | Clause | |
| :--- | ---: | ---: | ---: | :--- |
| Á. Valles | 55.00M | 44.77M | 55.00M | the price |
| Boyomo | 20.14M | 19.16M | 20.14M | the price |
| Álex Baena | 70.00M | 66.92M | 70.00M | the price |
| Canales | 27.00M | 25.98M | 27.00M | the price |
| Mujaid Sadick | 4.85M | 4.71M | 4.85M | the price |
| Bellingham | 115.31M | 116.22M | 116.22M | the value |
| Turrientes | 5.57M | 5.78M | 5.78M | the value |
| D. Cárdenas | 1.15M | 1.25M | 1.25M | the value |
| Mañas | 458K | 457K | 1.00M | the floor |

The consequence is the one worth remembering, because it inverts the intuition: **buying below
market value hands you the most exposed clause there is.** Pay 21.17M for a player worth 23.13M
and his clause is 23.13M — exactly 1.00x — while his value keeps climbing and the margin gets
worse every day. Paying *over* value is what buys protection. So the page prices a bargain with
its defence included, in the "Por debajo de su valor" section.

## The lock after a signing

336 hours. Exactly, in all six samples that had a lock still running:

| Player | Bought | Payable from | |
| :--- | :--- | :--- | ---: |
| Á. Valles | 04/09 23:06 | 18/09 23:06 | 336.0 h |
| Boyomo | 05/09 19:01 | 19/09 19:01 | 336.0 h |
| Álex Baena | 05/09 19:01 | 19/09 19:01 | 336.0 h |
| Turrientes | 06/09 19:03 | 20/09 19:03 | 336.0 h |
| Bellingham | 06/09 19:03 | 20/09 19:03 | 336.0 h |
| Canales | 06/09 19:03 | 20/09 19:03 | 336.0 h |

Fourteen days, which is why a new signing needs no defending yet and why the raise is worth
planning before that fortnight is up rather than after.

## Raising it

You pay an amount and the clause goes up by **twice** it (`writes.ClauseFactor`). So the
question is never how high it could go, it is where it stops being worth paying for somebody
else, and that has two answers, whichever is cheaper:

- **Out of reach**: above the richest rival's reconstructed cash. What he cannot pay he cannot
  pay, and past that number the extra euros defend against nobody.
- **No longer worth it**: above the price at which the player's points per million drop to what
  that rival's own squad already returns. Paying a clause to get *worse* value than you already
  have is not a raid anybody makes.

`advice.RaiseTarget` takes the cheaper of the two and `advice.ClausePlan` turns it into the
amount to pay, ranked by risk and cut off at the balance. Before spending anything it asks the
question that decides it: **what would losing him actually cost the pitch** — the drop in the
best legal eleven, not the player's own points. Under one expected point, defending him is worse
than banking the clause.

## The risk column

`advice.RaidRisk` is an estimate and says so on the page: it combines who can pay the clause at
all, how far above his own squad's rate the player would be at that price, and whether that
rival is short in the position. It is not a measured frequency — four matchdays is not a sample —
and the parts travel on the row so the number can be argued with.
