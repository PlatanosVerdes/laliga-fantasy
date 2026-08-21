# The daily reward, and why it is not automated yet

LaLiga Fantasy hands out a fixed amount once a day, per league, after watching an advert:
100.000 in a private league, 200.000 with premium. Over a season that is real money in a
game where the whole point is having cash on the right morning, so it is worth automating.
One thing is missing: the endpoint that claims it.

## What the API exposes

One route, read-only:

```
GET  /v1/competition/1/daily-reward
  [{"leagueType":"public","money":100000,"dailyLimit":1},
   {"leagueType":"private","money":100000,"dailyLimit":1},
   {"leagueType":"premium","money":200000,"dailyLimit":1}]

OPTIONS /v1/competition/1/daily-reward   →  405, allow: GET
```

That is the catalogue: how much a reward is worth and how many a day. It does not say whether
today's is still there, and it cannot claim it.

## Where the claim is not

About 60 candidate paths answered 404: league-scoped, team-scoped and user-scoped variants of
`daily-reward`, `dailyReward`, `daily_reward`, `reward`, `rewards`, `bonus`, `daily-bonus`,
`gift`, `prize`, `rewarded-ad`, `ad-reward`, and those with `claim`, `collect`, `redeem`,
`apply`, `take`, `receive`, `status` appended; under `/v1` … `/v4`; and on the older
`api-fantasy.llt-services.com` host as well.

The 404 is conclusive, which is the useful part. This API distinguishes a wrong path from a
wrong method: asking with the wrong verb answers **405 with an `allow:` header**, verified
against two routes known to exist.

```
GET /v1/competition/1/league/{league}/shield/player   →  405, allow: PUT
GET /v1/competition/1/league/{league}/market/sell     →  405, allow: POST
```

So a 404 means the path does not exist, not that the method was wrong. Guessing further is
not worth the requests.

Nothing else is left to read either: the old web client at `laligafantasy.relevo.com` now
redirects to relevo.com, the `fantasy.laliga.com` bundle is a landing page with no API calls
in it, and none of the public reverse-engineered projects implement the claim.

## The one thing that would unblock it

A single capture of the real request from the phone.

1. Proxyman on the Mac (the trial is enough), *iOS Device* in the welcome panel.
2. iPhone on the same Wi-Fi: Ajustes → Wi-Fi → (i) → Configurar proxy → Manual, with the
   Mac's address and port 9090.
3. Certificate: open `http://proxy.man/ssl` in Safari on the phone, install the profile, and
   **trust it** in Ajustes → General → Información → Ajustes de confianza de certificados.
   Skipping that last step is why a capture comes back encrypted.
4. In Proxyman, enable SSL proxying for `fantasy-api.llt-services.com` (or
   `*.llt-services.com:443`), and filter by `llt` to cut the noise.
5. In the app: Mercado → Recoger recompensa → Reclamar recompensa, and let the advert finish.
6. The call that fires right after the advert: *Copy as cURL Request*. Method, path, request
   body and response body are what matter; the `Authorization` header is not needed.

If the app pins its certificates, Proxyman decrypts nothing and the app fails with a network
error instead. That is the end of the cheap route.

## What to build with it

The advert is probably not an obstacle. This same API takes the advert as a plain flag in the
body on another feature, with no proof attached — from
[LaLigaApp](https://github.com/Externoak/LaLigaApp), which shields a player like this:

```js
api.put(`${CMP}/league/${leagueId}/shield/player`, {
  playerId, rewardedAdType: "Blindaje", rewardedAd: 1,
})
```

If the claim follows that shape, sending the flag is the whole job. Then:

- a `claim_daily_reward` entry in `internal/writes`, with the captured path and body;
- a step in the automatic cycle that runs it once a day, stamped in the state directory so a
  restart or a busy afternoon cannot claim twice, and only when the catalogue says there is
  something to claim;
- the amount in the log line, like every other automatic action.

One more thing it would fix: the README's cash model calls the daily reward "the one term the
log does not record". Claiming it ourselves means we know exactly when it happened.
