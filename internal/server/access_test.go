package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const (
	testTeam     = "team.cloudflareaccess.com"
	testAudience = "aud-tag"
)

type signer struct {
	id  string
	key *rsa.PrivateKey
}

func newSigner(t *testing.T, id string) signer {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return signer{id: id, key: key}
}

func (s signer) token(t *testing.T, claims map[string]any) string {
	segment := func(value any) string {
		blob, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(blob)
	}
	signed := segment(map[string]string{"alg": "RS256", "kid": s.id, "typ": "JWT"}) + "." + segment(claims)
	digest := sha256.Sum256([]byte(signed))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func certsServer(t *testing.T, published ...signer) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		var keys []map[string]string
		for _, s := range published {
			keys = append(keys, map[string]string{
				"kid": s.id, "kty": "RSA", "alg": "RS256",
				"n": base64.RawURLEncoding.EncodeToString(s.key.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes()),
			})
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": keys})
	}))
	t.Cleanup(server.Close)
	return server
}

func guarded(t *testing.T, certs *httptest.Server, now time.Time) (http.Handler, *bool) {
	keys := newAccessKeys(Access{TeamDomain: testTeam, Audience: testAudience})
	keys.certsURL = certs.URL
	keys.now = func() time.Time { return now }
	reached := new(bool)
	return keys.middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { *reached = true })), reached
}

func validClaims(now time.Time) map[string]any {
	return map[string]any{"aud": []string{testAudience}, "iss": "https://" + testTeam,
		"exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Minute).Unix()}
}

func TestOnlyRequestsThroughAccessReachThePanel(t *testing.T) {
	now := time.Now()
	cloudflare := newSigner(t, "current")
	forger := newSigner(t, "current")
	certs := certsServer(t, cloudflare)

	with := func(change func(map[string]any)) map[string]any {
		claims := validClaims(now)
		change(claims)
		return claims
	}
	for _, c := range []struct {
		name  string
		token string
		want  bool
	}{
		{"valid", cloudflare.token(t, validClaims(now)), true},
		{"audience as a single string", cloudflare.token(t, with(func(c map[string]any) { c["aud"] = testAudience })), true},
		{"no token", "", false},
		{"garbage", "not.a.token", false},
		{"signed by another key", forger.token(t, validClaims(now)), false},
		{"another application", cloudflare.token(t, with(func(c map[string]any) { c["aud"] = []string{"other"} })), false},
		{"another team", cloudflare.token(t, with(func(c map[string]any) { c["iss"] = "https://evil.cloudflareaccess.com" })), false},
		{"expired", cloudflare.token(t, with(func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() })), false},
		{"not yet valid", cloudflare.token(t, with(func(c map[string]any) { c["nbf"] = now.Add(time.Hour).Unix() })), false},
		{"no expiry", cloudflare.token(t, with(func(c map[string]any) { delete(c, "exp") })), false},
	} {
		handler, reached := guarded(t, certs, now)
		request := httptest.NewRequest(http.MethodGet, "/api/state", nil)
		if c.token != "" {
			request.Header.Set(AccessHeader, c.token)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if *reached != c.want {
			t.Errorf("%s: reached the panel %v, want %v (status %d)", c.name, *reached, c.want, recorder.Code)
		}
		if !c.want && recorder.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", c.name, recorder.Code)
		}
	}
}

// A token whose header names "none" must not skip the signature check.
func TestUnsignedTokensAreRefused(t *testing.T) {
	now := time.Now()
	handler, reached := guarded(t, certsServer(t, newSigner(t, "current")), now)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","kid":"current"}`))
	blob, _ := json.Marshal(validClaims(now))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(AccessHeader, header+"."+base64.RawURLEncoding.EncodeToString(blob)+".")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	if *reached {
		t.Error("an unsigned token reached the panel")
	}
}

func TestProbesNeedNoToken(t *testing.T) {
	for _, path := range []string{"/healthz", "/api/health"} {
		handler, reached := guarded(t, certsServer(t), time.Now())
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		if !*reached {
			t.Errorf("%s was refused", path)
		}
	}
}

// Cloudflare rotates its signing keys: a token under a new key id fetches the set again.
func TestRotatedKeysAreFetched(t *testing.T) {
	now := time.Now()
	old, rotated := newSigner(t, "old"), newSigner(t, "new")
	published := []signer{old}
	certs := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		certsServer(t, published...).Config.Handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(certs.Close)

	keys := newAccessKeys(Access{TeamDomain: testTeam, Audience: testAudience})
	keys.certsURL = certs.URL
	clock := now
	keys.now = func() time.Time { return clock }
	if err := keys.verify(old.token(t, validClaims(now))); err != nil {
		t.Fatalf("old key: %v", err)
	}
	published = []signer{old, rotated}
	clock = now.Add(2 * time.Minute)
	if err := keys.verify(rotated.token(t, validClaims(now))); err != nil {
		t.Fatalf("rotated key: %v", err)
	}
}

func TestAccessNeedsBothSettings(t *testing.T) {
	for _, c := range []struct {
		access Access
		valid  bool
	}{
		{Access{}, true},
		{Access{TeamDomain: testTeam, Audience: testAudience}, true},
		{Access{TeamDomain: testTeam}, false},
		{Access{Audience: testAudience}, false},
	} {
		if err := c.access.Validate(); (err == nil) != c.valid {
			t.Errorf("%+v: valid %v, want %v", c.access, err == nil, c.valid)
		}
	}
}
