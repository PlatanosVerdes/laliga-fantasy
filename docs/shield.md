# The shield

A shielded player cannot be clausulado by anybody: for 24 hours his buyout clause is simply not
payable. It costs no money, only an advert, and it expires on its own. There is no route to
remove one and none is needed, which is why the panel treats it as a countdown rather than a
state you turn on and off.

```
PUT /v1/competition/1/league/{league}/shield/player
  {"playerId":"42111060","rewardedAdType":"Blindaje","rewardedAd":1}
```

`playerId` is the squad-slot id (`playerTeamId`), not the player's own id, the same as a clause
rise. The advert is a flag with no proof attached, exactly like the daily reward
([docs/daily-reward.md](daily-reward.md)).

Who is shielded and until when comes from the squad payload, so it needs no request of its own:

```
GET /v1/competition/1/leagues/{league}/teams/{team}
  ... "isShielded": true, "shieldedEndDate": "2026-09-05T17:46:36+02:00"
```

## How the route was found

Guessing paths is what failed for the daily reward, and it fails here too: this API answers
**404 to a wrong verb**, so a POST to a route that only accepts PUT looks exactly like a route
that does not exist. `buyout/player` — which exists, it is where a clause rise goes — answers
404 to a GET.

What works is asking the same path with several verbs and reading the difference:

| Request | Answer |
|---|---|
| `POST /league/{L}/shield/player` | 405 Method Not Allowed |
| `PATCH`, `DELETE` on the same path | 405 Method Not Allowed |
| `PUT /league/{L}/shield/player`, garbage id | 404 Not Found |
| `POST /league/{L}/shield/zzzz` | 404 Not Found |

A 405 means the path is routed and the verb is not: the only verb that reached something else
was PUT. The confirmation came from a body left deliberately incomplete — `{"playerId": <a real
slot>}` and nothing more — which answered

```
400 {"code":400,"message":"The required option \"rewardedAd\" is missing."}
```

The handler named its own missing field, and no shield was spent finding out.

One caveat for the next probe of this kind: a control path matters. `POST /league/{L}/buyout/zzzz`
also answers 405, so anything under `buyout/` proves nothing on its own. `zzzz/player` and
`shield/zzzz` both answer 404, which is what makes the `shield/player` reading trustworthy.

## Scheduling it

The 24 hours are the whole problem: they are worth nothing during the hours nobody can pay a
clause anyway, and the matchday closes that window for three days at a time
([clause-window.md](clause-window.md)). Buying the shield on the Friday of a matchday spends an
advert on hours in which no rival could have touched the player.

So the shield is an appointment. `policies.Policy` carries `shield` and `shield_at`, the page
suggests the instant the window reopens, and `policies.ShieldPlan` acts on the hour:

- before it, the instruction waits and says which hour it is waiting for;
- more than `ShieldGrace` (2 h) after it, it stands down rather than covering the wrong day: a
  cycle can be missed, a whole day cannot be recovered;
- once bought, the instruction is cleared. Leaving it armed would buy another one tomorrow,
  and the shield already expires on its own.

It never spends money, which is why it is not in `Spends` and needs no cap: what is being
authorised is the moment.

## In the tool

`internal/writes` has the operation (`shield_player`) and it goes through the same two-step
guard as the rest: prepare, read the summary, confirm. It refuses to shield a player who
already is, naming the hour the current one runs out, because the second advert would buy
nothing.

`fantasy raid list` and the page's two clause sections show the window; the drawer of one of
your own players has both buttons, "Blindar 24h ahora" and "Programar blindaje".

There is no limit endpoint. `/shield`, `/shields` and every `check-shield` shape answer 404, so
whether the game caps how many can be shielded in a day is not knowable from here: the answer
will arrive as a refusal on the day it is hit, and the message travels to the page.
