package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
)

const (
	AuthorizeURL      = "https://login.laliga.es/laligadspprob2c.onmicrosoft.com/oauth2/v2.0/authorize"
	NativeRedirectURI = "authredirect://com.lfp.laligafantasy"
	// The pending verifier is only useful for as long as somebody is in front of the browser.
	PendingTTL = 15 * time.Minute
)

// Pending is a login in flight: the PKCE verifier and the state, which have to survive
// between building the URL and pasting the redirect back.
type Pending struct {
	Verifier string  `json:"verifier"`
	State    string  `json:"state"`
	Started  float64 `json:"started"`
	URL      string  `json:"url"`
}

func pendingPath() string { return filepath.Join(config.ConfigDir, "pending_auth.json") }

// StartLogin builds the authorize URL for the app's own client (auth code + PKCE).
//
// The mobile app registers the custom scheme authredirect://com.lfp.laligafantasy, which a
// desktop browser cannot open — but it still puts the redirect, with the authorization code in
// the query string, in the address bar. That is enough to finish the exchange locally, and
// unlike the web client this flow accepts the federated logins (Google, Apple, Facebook) that
// the app offers.
func StartLogin() (*Pending, error) {
	verifier, err := randomURLSafe(40)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	state, err := randomURLSafe(16)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("p", config.B2CSignInPolicy)
	params.Set("client_id", config.B2CNativeClientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", NativeRedirectURI)
	params.Set("scope", "openid offline_access")
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("state", state)
	// Same value as state: B2C wants a nonce and there is nothing else to bind it to here.
	params.Set("nonce", state)

	pending := &Pending{Verifier: verifier, State: state,
		Started: float64(time.Now().Unix()),
		URL:     AuthorizeURL + "?" + params.Encode()}

	if err := config.EnsureDirs(); err != nil {
		return nil, err
	}
	blob, err := json.Marshal(pending)
	if err != nil {
		return nil, err
	}
	// 0600: the verifier is half of a credential until the exchange happens.
	if err := os.WriteFile(pendingPath(), blob, 0o600); err != nil {
		return nil, err
	}
	return pending, nil
}

func LoadPending() (*Pending, error) {
	body, err := os.ReadFile(pendingPath())
	if err != nil {
		return nil, err
	}
	var pending Pending
	if err := json.Unmarshal(body, &pending); err != nil {
		return nil, err
	}
	if time.Since(time.Unix(int64(pending.Started), 0)) > PendingTTL {
		return nil, errors.New("el login empezado ha caducado: pide otro enlace")
	}
	return &pending, nil
}

// ExtractCode pulls the authorization code out of a pasted redirect URL, or accepts a bare
// code. The code is a JWE and single-use, so a mismatched state means starting again rather
// than retrying.
func ExtractCode(text, expectedState string) (string, error) {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, `"'`)
	if text == "" {
		return "", errors.New("no se ha pegado nada")
	}
	if !strings.Contains(text, "code=") && !strings.Contains(text, "?") {
		return text, nil
	}

	query := text
	if parsed, err := url.Parse(text); err == nil && parsed.RawQuery != "" {
		query = parsed.RawQuery
	} else if index := strings.Index(text, "?"); index >= 0 {
		query = text[index+1:]
	}
	params, err := url.ParseQuery(query)
	if err != nil {
		return "", err
	}
	if failure := params.Get("error"); failure != "" {
		reason := params.Get("error_description")
		if reason == "" {
			reason = failure
		}
		return "", fmt.Errorf("B2C ha devuelto un error: %s", reason)
	}
	code := params.Get("code")
	if code == "" {
		return "", errors.New("la URL pegada no contiene ningun `code=`")
	}
	if got := params.Get("state"); expectedState != "" && got != "" && got != expectedState {
		return "", errors.New("el `state` no coincide: reinicia el login")
	}
	return code, nil
}

// ExchangeCode finishes the login and stores the session.
func ExchangeCode(code, verifier string) (*Tokens, error) {
	var answer map[string]any
	err := httpx.PostForm(config.B2CTokenURL+"?p="+config.B2CSignInPolicy, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     config.B2CNativeClientID,
		"code":          code,
		"redirect_uri":  NativeRedirectURI,
		"code_verifier": verifier,
		"scope":         "openid offline_access",
	}, &answer)
	if err != nil {
		return nil, err
	}
	answer["client_id"] = config.B2CNativeClientID
	tokens := Normalize(answer)
	if tokens.AccessToken == "" {
		return nil, errors.New("la respuesta no traia ningun token")
	}
	if err := Save(tokens); err != nil {
		return nil, err
	}
	// The pending verifier is spent either way.
	_ = os.Remove(pendingPath())
	return tokens, nil
}

func randomURLSafe(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Adopt takes a pasted tokens.json — the fastest way out of a broken session when there is
// already a working one on another machine — and stores it after checking it is usable.
func Adopt(blob string) (*Tokens, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(blob), &raw); err != nil {
		return nil, fmt.Errorf("eso no es un JSON valido: %w", err)
	}
	tokens := Normalize(raw)
	if tokens.RefreshToken == "" && tokens.AccessToken == "" {
		return nil, errors.New("el JSON no trae ni access_token ni refresh_token")
	}
	if err := Save(tokens); err != nil {
		return nil, err
	}
	return tokens, nil
}
