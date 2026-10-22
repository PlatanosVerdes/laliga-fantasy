// Package auth keeps the session alive. Mirrors fantasy/auth.py: the same file, the
// same field names, the same tight refresh margin.
//
// The session cannot live in an environment variable, because the refresh token
// rotates: the provider can hand back a new one on every renewal and it has to be
// written somewhere. That is why gh, aws and kubectl all keep rotating credentials in a
// file. A container still needs a way to be handed the first one, so FANTASY_TOKENS or
// FANTASY_REFRESH_TOKEN seeds the file once and rotation takes over from there.
package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/files"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/secret"
)

// Each refresh is a hit on LaLiga's identity provider and can trigger a security
// notice, so the margin is deliberately tight and a cooldown stops a burst of calls
// from renewing more than once.
const (
	RefreshMargin   = 2 * time.Minute
	RefreshCooldown = 10 * time.Minute
	// MaxFailedRefreshes is how many renewals in a row may fail before the panel stops asking:
	// a revoked session retried forever is a burst of security notices and a locked account.
	MaxFailedRefreshes = 3
)

// ErrRejected is the provider turning the renewal down (a 4xx such as invalid_grant), as
// opposed to not being reachable: only this counts towards stopping, so a night without internet
// does not end a session that is fine.
var ErrRejected = errors.New("el proveedor ha rechazado la renovacion")

// ErrStopped is the session given up on after MaxFailedRefreshes: only a new login restarts it.
var ErrStopped = errors.New("la renovacion del login ha fallado 3 veces seguidas: " +
	"el panel no vuelve a intentarlo hasta que inicies sesion de nuevo")

// Tokens is the stored session. The JSON names match the Python file exactly, because
// both implementations read and write the same tokens.json.
type Tokens struct {
	AccessToken  string  `json:"access_token"`
	IDToken      string  `json:"id_token,omitempty"`
	RefreshToken string  `json:"refresh_token,omitempty"`
	ClientID     string  `json:"client_id,omitempty"`
	ExpiresOn    int64   `json:"expires_on"`
	Email        string  `json:"email,omitempty"`
	Name         string  `json:"name,omitempty"`
	IDP          string  `json:"idp,omitempty"`
	UserID       string  `json:"user_id,omitempty"`
	RefreshedAt  float64 `json:"refreshed_at,omitempty"`
	// FailedRefreshes counts renewals failed in a row and LastAttempt is the latest, failed or
	// not: the cooldown runs from it, so a failing renewal is not retried on every call.
	FailedRefreshes int     `json:"failed_refreshes,omitempty"`
	LastAttempt     float64 `json:"last_attempt,omitempty"`
}

// Stopped is whether the session was given up on.
func (t *Tokens) Stopped() bool { return t != nil && t.FailedRefreshes >= MaxFailedRefreshes }

func (t *Tokens) SecondsLeft() int64 {
	if t == nil {
		return 0
	}
	return t.ExpiresOn - time.Now().Unix()
}

var mu sync.Mutex

// Load reads the stored session, seeding it from the environment on first run.
func Load() (*Tokens, error) {
	tokens, err := read(config.TokenFile)
	if errors.Is(err, os.ErrNotExist) {
		return seedFromEnv()
	}
	return tokens, err
}

func LoadAccount(account string) (*Tokens, error) {
	return read(config.AccountTokenFile(account))
}

func Save(tokens *Tokens) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}
	return write(config.TokenFile, tokens)
}

func read(path string) (*Tokens, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	plain := raw
	sealed := secret.Sealed(raw)
	if sealed {
		if plain, err = secret.Open(raw); err != nil {
			return nil, fmt.Errorf("no se puede leer la sesion %s: %w", path, err)
		}
	}
	var tokens Tokens
	if err := json.Unmarshal(plain, &tokens); err != nil {
		return nil, fmt.Errorf("tokens.json no es un JSON valido: %w", err)
	}
	if !sealed {
		sealInPlace(path, raw)
	}
	return &tokens, nil
}

func sealInPlace(path string, plain []byte) {
	mu.Lock()
	defer mu.Unlock()
	// A renewal saved since it was read is newer: sealing the old bytes would roll it back.
	if current, err := os.ReadFile(path); err != nil || !bytes.Equal(current, plain) {
		return
	}
	sealed, err := secret.Seal(plain)
	if err == nil {
		err = files.WriteAtomic(path, sealed, 0o600)
	}
	if err != nil {
		slog.Warn("session file left unencrypted", "file", path, "reason", err.Error())
		return
	}
	slog.Info("session file encrypted", "file", path)
}

func write(path string, tokens *Tokens) error {
	blob, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	sealed, err := secret.Seal(blob)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	return files.WriteAtomic(path, sealed, 0o600)
}

func seedFromEnv() (*Tokens, error) {
	if blob := os.Getenv("FANTASY_TOKENS"); blob != "" {
		var raw map[string]any
		if err := json.Unmarshal([]byte(blob), &raw); err != nil {
			return nil, fmt.Errorf("FANTASY_TOKENS no es un JSON valido: %w", err)
		}
		tokens := Normalize(raw)
		if _, err := Store(tokens); err != nil {
			return nil, err
		}
		slog.Info("session seeded from FANTASY_TOKENS")
		return tokens, nil
	}
	if token := os.Getenv("FANTASY_REFRESH_TOKEN"); token != "" {
		tokens, err := Refresh(&Tokens{RefreshToken: token})
		if err != nil {
			return nil, fmt.Errorf("no se ha podido usar FANTASY_REFRESH_TOKEN: %w", err)
		}
		if _, err := Store(tokens); err != nil {
			return nil, err
		}
		slog.Info("session seeded from FANTASY_REFRESH_TOKEN")
		return tokens, nil
	}
	return nil, nil
}

// Normalize turns whatever the provider answered into the stored shape, taking the
// expiry from the token's own claims when the response does not say.
func Normalize(raw map[string]any) *Tokens {
	access := str(raw["access_token"])
	if access == "" {
		access = str(raw["id_token"])
	}
	claims := decodeJWT(access)

	expiresOn := num(raw["expires_on"])
	if expiresOn == 0 {
		if exp := num(claims["exp"]); exp != 0 {
			expiresOn = exp
		} else {
			seconds := num(raw["expires_in"])
			if seconds == 0 {
				seconds = 86400
			}
			expiresOn = float64(time.Now().Unix()) + seconds
		}
	}

	clientID := str(raw["client_id"])
	if clientID == "" {
		clientID = str(claims["aud"])
	}
	if clientID == "" {
		clientID = config.B2CWebClientID
	}

	idToken := str(raw["id_token"])
	if idToken == "" {
		idToken = access
	}
	email := str(claims["email"])
	if email == "" {
		email = str(claims["unique_name"])
	}
	name := str(claims["name"])
	if name == "" {
		name = str(claims["given_name"])
	}

	return &Tokens{
		AccessToken:  access,
		IDToken:      idToken,
		RefreshToken: str(raw["refresh_token"]),
		ClientID:     clientID,
		ExpiresOn:    int64(expiresOn),
		Email:        email,
		Name:         name,
		IDP:          str(claims["idp"]),
		UserID:       userOf(claims, email),
		RefreshedAt:  num(raw["refreshed_at"]),
	}
}

// Refresh renews the session, trying the client ids in the same order as Python: the
// stored one first, then the web app's, then the mobile app's. Which one works has
// changed before, and the answer is not worth a round of guessing at 3am.
func Refresh(tokens *Tokens) (*Tokens, error) {
	if tokens == nil || tokens.RefreshToken == "" {
		return nil, errors.New("no hay refresh_token guardado: vuelve a ejecutar `auth browser`")
	}
	url := config.B2CTokenURL + "?p=" + config.B2CSignInPolicy

	var failures []string
	rejected := true
	for _, clientID := range dedupe(tokens.ClientID, config.B2CWebClientID, config.B2CNativeClientID) {
		var answer map[string]any
		err := httpx.PostForm(url, map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": tokens.RefreshToken,
			"client_id":     clientID,
			"scope":         "openid offline_access",
		}, &answer)
		if err != nil {
			var httpErr *httpx.Error
			if errors.As(err, &httpErr) {
				failures = append(failures, fmt.Sprintf("%s: %d", clientID, httpErr.Status))
				rejected = rejected && httpErr.Status >= 400 && httpErr.Status < 500
				continue
			}
			return nil, err
		}
		answer["client_id"] = clientID
		if str(answer["refresh_token"]) == "" {
			// The provider does not always rotate it; keep the one that still works.
			answer["refresh_token"] = tokens.RefreshToken
		}
		fresh := Normalize(answer)
		fresh.RefreshedAt = float64(time.Now().Unix())
		if err := Save(fresh); err != nil {
			return nil, err
		}
		slog.Info("token refreshed", "client_id", clientID,
			"expires_in_min", fresh.SecondsLeft()/60)
		return fresh, nil
	}
	err := fmt.Errorf("el refresh ha fallado con todos los client_id: %s", strings.Join(failures, ", "))
	if rejected {
		return nil, fmt.Errorf("%w: %w", ErrRejected, err)
	}
	return nil, err
}

// renew is Refresh, swapped out in tests so a failing provider needs no network.
var renew = Refresh

// bearerMu makes check, renew and save one step: two requests near the expiry each renewed the
// same refresh token.
var bearerMu sync.Mutex

// Bearer is the current access token, refreshing only when it is genuinely about to
// die.
func Bearer() (string, error) {
	bearerMu.Lock()
	defer bearerMu.Unlock()
	tokens, err := Load()
	if err != nil {
		return "", err
	}
	if tokens == nil {
		return "", errors.New("sin sesion guardada. Ejecuta `auth browser` y luego `auth code '<url>'`")
	}
	// Under FANTASY_FREEZE nothing may reach the network, so renewing is both impossible
	// and pointless: every answer comes from the cache. Without this, a snapshot stops
	// being replayable the moment the session inside it expires.
	if httpx.Frozen {
		return tokens.AccessToken, nil
	}

	if tokens.Stopped() {
		return "", ErrStopped
	}

	if left := tokens.SecondsLeft(); left < int64(RefreshMargin.Seconds()) && tokens.RefreshToken != "" {
		since := time.Since(time.Unix(int64(max(tokens.RefreshedAt, tokens.LastAttempt)), 0))
		if since >= RefreshCooldown {
			if fresh, err := renew(tokens); err == nil {
				tokens = fresh
			} else {
				if errors.Is(err, ErrRejected) {
					tokens.FailedRefreshes++
				}
				tokens.LastAttempt = float64(time.Now().Unix())
				if saveErr := Save(tokens); saveErr != nil {
					slog.Error("could not record a failed refresh", "reason", saveErr.Error())
				}
				if tokens.Stopped() {
					slog.Error("login stopped after failed refreshes", "failed", tokens.FailedRefreshes,
						"reason", err.Error())
					return "", ErrStopped
				}
				slog.Warn("refresh failed, using the token we have", "failed", tokens.FailedRefreshes,
					"reason", err.Error())
			}
		} else {
			slog.Debug("refresh skipped by cooldown", "seconds_since", int(since.Seconds()))
		}
	}
	if tokens.SecondsLeft() < 0 {
		return "", errors.New("el token ha caducado y no se ha podido refrescar; vuelve a ejecutar `auth browser`")
	}
	return tokens.AccessToken, nil
}

func (t *Tokens) User() string {
	if t == nil {
		return ""
	}
	if t.UserID != "" {
		return t.UserID
	}
	claims := decodeJWT(t.AccessToken)
	if len(claims) == 0 {
		claims = decodeJWT(t.IDToken)
	}
	return userOf(claims, t.Email)
}

// B2C fills sub with a sentence when the policy does not issue it, hence the space check.
func userOf(claims map[string]any, email string) string {
	for _, key := range []string{"oid", "sub"} {
		if id := str(claims[key]); id != "" && !strings.Contains(id, " ") {
			return id
		}
	}
	return strings.ToLower(email)
}

func AccountUser(account string) string {
	tokens, err := LoadAccount(account)
	if err != nil {
		return ""
	}
	return tokens.User()
}

func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return map[string]any{}
	}
	claims := map[string]any{}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return map[string]any{}
	}
	return claims
}

func dedupe(values ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func str(value any) string {
	text, _ := value.(string)
	return text
}

func num(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case string:
		var parsed float64
		if _, err := fmt.Sscanf(typed, "%f", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}
