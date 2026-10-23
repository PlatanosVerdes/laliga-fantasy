# Logging in

LaLiga authenticates through an Azure AD B2C tenant (`login.laliga.es`). Access tokens last
24h and league endpoints reject anonymous calls.

## `auth browser` — the one that works app-only

LaLiga Fantasy is effectively app-only: <https://fantasy.laliga.com/> is a landing page, not
a web client, so there is no browser session to lift tokens from. If you do not have an
account yet, create it in the mobile app first
([iOS](https://apps.apple.com/app/id968915185) /
[Android](https://play.google.com/store/apps/details?id=com.lfp.laligafantasy)).

The way in is the **mobile app's own OAuth client**, which accepts the authorization-code +
PKCE flow from any desktop browser, federated logins included. Two commands, because the
browser step happens in between:

```bash
./fantasy auth browser          # prints the URL, stores the PKCE verifier
./fantasy auth code '<url>'     # exchanges the code the browser redirected to
```

`serve` does the same thing without a terminal: with no session, the page itself shows the
login link and a box to paste the redirect into, which is how a fresh deploy authenticates.

**Step 1 — get the URL.** `auth browser` builds it and saves the PKCE verifier and `state`
to `pending_auth.json` in the config directory (valid 15 minutes). It looks like this, with the app's client id
and its native redirect:

```
https://login.laliga.es/laligadspprob2c.onmicrosoft.com/oauth2/v2.0/authorize
  ?p=B2C_1A_5ULAIP_PARAMETRIZED_SIGNIN
  &client_id=af88bcff-1157-40a0-b579-030728aacf0b
  &response_type=code
  &redirect_uri=authredirect%3A%2F%2Fcom.lfp.laligafantasy
  &scope=openid+offline_access
  &code_challenge=<generated>&code_challenge_method=S256
  &state=<generated>&nonce=<generated>
```

Use the URL the command prints, not this one: the challenge and state are generated per
attempt and the exchange fails without the matching verifier.

**Step 2 — open DevTools _before_ logging in.** This is the part that catches everyone out.
In Chrome: `Cmd+Opt+I` / `Ctrl+Shift+I` → **Network** tab → tick **Preserve log**. Then paste
the URL and log in.

**Step 3 — log in.** The page offers Google / Apple / Facebook, the same options as the app.
Google may then ask for extra scopes on LaLiga's behalf (YouTube, gender, birth date, postal
address): **leave every box unchecked**, none of it is used here, and click Continue.

**Step 4 — grab the code from the Network log.** The browser now tries to open
`authredirect://com.lfp.laligafantasy` and **hangs on a blank page with the address bar
unchanged** — Chrome cannot handle a custom scheme, and this is what success looks like. The
redirect is in the Network log as the **last row, status `(canceled)`**, its name starting
with `?state=…&code=…`:

```
authresp?state=StateProperties%3D…        302   document   3.9 kB
?state=hYTieapYtmRTbp_dfsN5JQ&cod…  (canceled)  document   0.0 kB   ← this one
```

Right-click it → **Copy → Copy link address**. (Or click the row → **Headers** → copy
**Request URL**.) You get the full redirect:

```
authredirect://com.lfp.laligafantasy/?state=hYTieap…&code=eyJraWQiOiJDandFaWN0…
```

**Step 5 — exchange it.** Quote it: the `&` would otherwise split in the shell.

```bash
./fantasy auth code 'authredirect://com.lfp.laligafantasy/?state=…&code=…'
# Sesion guardada. tu@email · caduca en 1439 min · refresh: si
```

The `code` is a JWE and single-use, so if the exchange fails for any reason other than a
`state` mismatch, start again from step 1.

Splitting the flow in two also means neither command needs a TTY, so it works over ssh.
`state` is verified and B2C errors are surfaced verbatim. From then on `auth refresh` — called
automatically when the token is within 2 minutes of expiry, at most once every 10 minutes —
keeps the session alive indefinitely, exactly as the app does; you should never have to log in
again. `auth status` shows what is stored. Tokens live in `tokens.json`, mode `0600`, in the config
directory — see [Where things live](data.md).

## Encrypted at rest

`tokens.json` and the cookie and action id in `necroporra.json` are sealed with AES-256-GCM, so
a copy of the data folder alone opens no session. The file is a small envelope,
`{"v":1,"kid":…,"nonce":…,"data":…}`; in `necroporra.json` only those two fields are, and the
gameweek and roster stay editable. A plaintext file (an older install, a fresh cookie pasted in)
is accepted and sealed in place on its first read.

The key, in this order:

* **`FANTASY_SECRET_KEY`**: 32 bytes in base64 or hex (`openssl rand -base64 32`).
* **`FANTASY_SECRET_FILE`**, by default `secret.key` at the config root, created `0600` on the
  first start when missing.

Only a key kept outside the data folder (the variable, from a secrets manager) protects a
copied folder; the file default is for a zero-config install and sits right beside what it
seals. Either way, **back up the key**: without it the sessions cannot be read and every account
has to log in again. A file sealed under another key is never overwritten by a read: the panel
logs `la clave no coincide` and asks for a login, and only that login replaces it. `auth export`
prints the decrypted session, for moving it to another machine.

## Other routes

* Pasting a whole `tokens.json` into the page's login box works too, and is checked against
  `/user/me` before it is accepted. Useful when a session already exists on another machine:
  `auth export` there prints it decrypted.
