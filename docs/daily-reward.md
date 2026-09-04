# The daily reward

LaLiga Fantasy hands out a fixed amount once a day, per league, after watching an advert:
100.000 in a public or private league, 200.000 with premium. Over a season that is real money
in a game where the whole point is having cash on the right morning, so the engine claims it.

Captured from the phone with Proxyman on 2026-08-24, which is the only reason any of this is
written down: none of it is documented, and about 60 guessed paths had answered 404 before.

## The three routes

```
GET  /v1/competition/1/daily-reward
  [{"leagueType":"public","money":100000,"dailyLimit":1},
   {"leagueType":"private","money":100000,"dailyLimit":1},
   {"leagueType":"premium","money":200000,"dailyLimit":1}]

GET  /v1/competition/1/league/{league}/team/{team}/check-daily-reward
  {"teamId":38126981,"dailyRewardsRedeemed":0}          ← queda por cobrar
  400 {"errorCode":"050.01.04","message":"Team daily reward limit reached"}   ← ya cobrada

POST /v1/competition/1/league/{league}/team/daily-reward
  {"rewardedAdType":"dailyreward","rewardedAd":1,"teamId":"38126981"}
```

Two things about the POST are worth keeping in mind, because they are why the guessing failed:
the path **stops at the league** — the team id travels in the body, not in the path, so every
team-scoped path answered 404 — and the segment is `team`, singular, while standings and squads
hang off `leagues` plural. The team id is a string in the body and a number in the check's
response.

The advert is a flag. The API takes `rewardedAd: 1` and attaches no proof to it, exactly like
the shield does (`rewardedAdType: "Blindaje"`, see [shield.md](shield.md)). Watching the video
is the app's business, not the server's.

`check-daily-reward` answering 400 instead of a counter of one is the useful part: the absence
of a body is the answer, and it means the claim cannot be attempted twice by accident. That
refusal is not retried — `httpx` treats a 400 like a 404 and gives up on the first one.

## How the engine claims it

`internal/writes` has the operation (`claim_daily_reward`), and the automatic cycle calls it
before the standing instructions, in `cmd/fantasy/main.go`. It is deliberately outside
`policies`: there is no amount and no limit to authorise in advance, only a day.

The order is check, claim, stamp:

- `rewards.ClaimedToday(league)` short-circuits the whole thing, so the rest of the day costs
  no requests at all. The stamp lives in `daily_reward.json` in the state directory, keyed by
  league, and the day is Madrid's, because that is when the counter resets. The zone database
  travels inside the binary (`_ "time/tzdata"`), which is not optional: the runtime image is
  alpine and has no `/usr/share/zoneinfo`, so `LoadLocation` failed, the day fell back to
  UTC's, and the claim landed at 02:08 Madrid instead of just after midnight.
- `check-daily-reward` decides. A 400 or a counter above zero stamps the day and stops:
  claiming from the phone must not have the engine asking again every two minutes.
- the amount is not in the response, so it is the difference in `/money` around the claim, and
  that is what goes in the log line.

By hand: `fantasy reward` says what it is worth and whether today's is still there, and
`fantasy reward --claim` claims it.

## Capturing it again

If the shape ever changes, the capture is the way back, and two things about the setup are not
obvious:

- Proxyman serves its CA as a raw `.pem` at `http://proxy.man/ssl` (no `www.`, that host
  answers nothing). Depending on the iOS version that lands in Files, where a CA cannot be
  installed. Wrapping the certificate in a `.mobileconfig` with a `com.apple.security.root`
  payload and serving it as `application/x-apple-aspen-config` always works.
- With a manual proxy on the phone, the **Mac** resolves every name. A Pi-hole on the Mac's DNS
  path therefore kills the advert, the app says there is nothing to watch, and no claim is ever
  fired. `pihole disable 10m` for the length of the capture, and it re-enables itself.
