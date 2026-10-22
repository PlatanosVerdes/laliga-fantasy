package server

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type doorKey struct{}

func doorOf(request *http.Request) string {
	if door, ok := request.Context().Value(doorKey{}).(string); ok {
		return door
	}
	return "private"
}

// AccessHeader carries the token Cloudflare Access signs for every request it lets through.
const AccessHeader = "Cf-Access-Jwt-Assertion"

// Access is the door the Cloudflare tunnel uses: a port of its own where a request without a
// valid token is refused, because it did not come through Access and the panel can place
// real bids.
type Access struct {
	Port       int
	TeamDomain string
	Audience   string
}

func (a Access) Enabled() bool { return a.Port != 0 }

// Validate refuses a half-configured door rather than opening it without the check.
func (a Access) Validate() error {
	set := 0
	for _, given := range []bool{a.Port != 0, a.TeamDomain != "", a.Audience != ""} {
		if given {
			set++
		}
	}
	if set != 0 && set != 3 {
		return errors.New("Cloudflare Access needs the port, the team domain and the audience tag")
	}
	return nil
}

type accessKeys struct {
	certsURL string
	issuer   string
	audience string
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func newAccessKeys(access Access) *accessKeys {
	issuer := "https://" + access.TeamDomain
	return &accessKeys{
		certsURL: issuer + "/cdn-cgi/access/certs",
		issuer:   issuer,
		audience: access.Audience,
		client:   &http.Client{Timeout: 10 * time.Second},
		now:      time.Now,
	}
}

// requireAccess lets through only requests carrying a valid Access token. The health
// endpoints are exempt: the probes reach them over the Docker network, never through Access.
func requireAccess(access Access, next http.Handler) http.Handler {
	return newAccessKeys(access).middleware(next)
}

func (k *accessKeys) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" || request.URL.Path == "/api/health" {
			next.ServeHTTP(writer, request)
			return
		}
		if err := k.verify(request.Header.Get(AccessHeader)); err != nil {
			slog.Warn("request without a valid Access token", "path", request.URL.Path,
				"reason", err.Error())
			http.Error(writer, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), doorKey{}, "access")))
	})
}

type accessClaims struct {
	Audience  audience `json:"aud"`
	Issuer    string   `json:"iss"`
	Expires   int64    `json:"exp"`
	NotBefore int64    `json:"nbf"`
}

// audience is a JWT "aud", which may be one string or a list of them.
type audience []string

func (a *audience) UnmarshalJSON(blob []byte) error {
	var one string
	if json.Unmarshal(blob, &one) == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(blob, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (k *accessKeys) verify(token string) error {
	if token == "" {
		return errors.New("no token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("malformed token")
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return fmt.Errorf("header: %w", err)
	}
	if header.Algorithm != "RS256" {
		return fmt.Errorf("algorithm %q", header.Algorithm)
	}
	key, err := k.key(header.KeyID)
	if err != nil {
		return err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("signature: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return errors.New("bad signature")
	}
	var claims accessClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return fmt.Errorf("claims: %w", err)
	}
	now := k.now().Unix()
	switch {
	case claims.Issuer != k.issuer:
		return fmt.Errorf("issuer %q", claims.Issuer)
	case !slices.Contains(claims.Audience, k.audience):
		return errors.New("audience mismatch")
	case claims.Expires == 0 || now >= claims.Expires:
		return errors.New("expired")
	case claims.NotBefore != 0 && now < claims.NotBefore:
		return errors.New("not yet valid")
	}
	return nil
}

// key returns the signing key by id, refreshing the set when the id is new: Cloudflare
// rotates its keys. Refreshes are spaced so a flood of forged ids cannot hammer the endpoint.
func (k *accessKeys) key(id string) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if key, ok := k.keys[id]; ok {
		return key, nil
	}
	if k.now().Sub(k.fetched) < time.Minute {
		return nil, fmt.Errorf("unknown key %q", id)
	}
	k.fetched = k.now()
	keys, err := k.fetch()
	if err != nil {
		return nil, fmt.Errorf("fetching Access keys: %w", err)
	}
	k.keys = keys
	if key, ok := keys[id]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key %q", id)
}

func (k *accessKeys) fetch() (map[string]*rsa.PublicKey, error) {
	response, err := k.client.Get(k.certsURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", response.StatusCode)
	}
	var set struct {
		Keys []struct {
			KeyID    string `json:"kid"`
			KeyType  string `json:"kty"`
			Modulus  string `json:"n"`
			Exponent string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(response.Body).Decode(&set); err != nil {
		return nil, err
	}
	keys := map[string]*rsa.PublicKey{}
	for _, entry := range set.Keys {
		if entry.KeyType != "RSA" {
			continue
		}
		modulus, errN := base64.RawURLEncoding.DecodeString(entry.Modulus)
		exponent, errE := base64.RawURLEncoding.DecodeString(entry.Exponent)
		if errN != nil || errE != nil {
			continue
		}
		keys[entry.KeyID] = &rsa.PublicKey{
			N: new(big.Int).SetBytes(modulus),
			E: int(new(big.Int).SetBytes(exponent).Int64()),
		}
	}
	return keys, nil
}

func decodeSegment(segment string, into any) error {
	blob, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		return err
	}
	return json.Unmarshal(blob, into)
}
