// Package httpx is the one place that talks to the network: a cache keyed by tag and
// URL, a request counter, and the retry rules. It mirrors fantasy/http.py, including
// the parts that look like details and are not — 429 is an answer rather than a glitch,
// and a probe needs to store a response without reading from the cache.
package httpx

import (
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// Stats counts requests actually put on the wire, so a refresh policy can be measured
// instead of argued about.
type Counters struct {
	Requests  int `json:"requests"`
	CacheHits int `json:"cache_hits"`
	Errors    int `json:"errors"`
}

var (
	mu    sync.Mutex
	stats Counters
)

func Stats() Counters {
	mu.Lock()
	defer mu.Unlock()
	return stats
}

func bump(field *int) {
	mu.Lock()
	*field++
	mu.Unlock()
}

// Error is a non-retryable HTTP answer.
type Error struct {
	Status int
	URL    string
	Body   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("http %d for %s", e.Status, e.URL)
}

// RateLimited is 429. Retrying makes it worse, so it is surfaced for the caller to
// stop the cycle rather than retried.
type RateLimited struct {
	URL        string
	RetryAfter string
}

func (e *RateLimited) Error() string {
	return "rate limited by " + e.URL
}

// Request is one fetch. TTL reads from the cache; Store writes to it without reading,
// which is what a probe wants: always ask, but leave the answer where the rest of the
// cycle will find it.
type Request struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
	TTL     time.Duration
	Store   bool
	Tag     string
	Timeout time.Duration
	Retries int
}

// Frozen is the differential harness's guarantee: TTLs are ignored and the network is
// refused, so a missing entry fails loudly instead of being fetched — which would make
// the two implementations read different bytes and compare nothing.
var Frozen = os.Getenv("FANTASY_FREEZE") == "1" ||
	strings.EqualFold(os.Getenv("FANTASY_FREEZE"), "true") ||
	strings.EqualFold(os.Getenv("FANTASY_FREEZE"), "yes")

func init() {
	if Frozen {
		// Loud on purpose: frozen mode refuses the network and therefore does not renew
		// the session. Set by accident in a real run it would serve stale data and let the
		// token die quietly.
		slog.Warn("FANTASY_FREEZE activo: no se toca la red, no se renueva la sesion, " +
			"todo sale de la cache")
	}
}

// FrozenMiss is asked for something the frozen cache does not hold.
type FrozenMiss struct {
	URL string
	Tag string
}

func (e *FrozenMiss) Error() string {
	return "FANTASY_FREEZE: no hay nada en cache para " + e.Tag + " " + e.URL
}

func cachePath(rawURL, tag string) string {
	sum := sha1.Sum([]byte(tag + "|" + rawURL))
	return filepath.Join(config.CacheDir, tag+"_"+hex.EncodeToString(sum[:])[:20]+".cache")
}

func readCache(rawURL, tag string, ttl time.Duration) (string, bool) {
	if ttl <= 0 && !Frozen {
		return "", false
	}
	path := cachePath(rawURL, tag)
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	if !Frozen && time.Since(info.ModTime()) > ttl {
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(body), true
}

func writeCache(rawURL, tag, body string) {
	if err := config.EnsureDirs(); err != nil {
		return
	}
	_ = os.WriteFile(cachePath(rawURL, tag), []byte(body), 0o600)
}

// Cached is a cache-only read, so callers can throttle just the real requests.
func Cached(rawURL, tag string, ttl time.Duration) (string, bool) {
	return readCache(rawURL, tag, ttl)
}

// Invalidate drops cached responses by tag, so the next read goes to the wire. Used
// when something just learned makes a long TTL wrong: a transfer means every squad is
// stale however recently it was read.
func Invalidate(tags ...string) int {
	dropped := 0
	for _, tag := range tags {
		if !config.Tags[tag] {
			// A misspelled tag drops no files and still reports success, which would
			// leave a write believing it had invalidated data it had not.
			slog.Error("invalidating a cache tag that does not exist", "tag", tag)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(config.CacheDir, tag+"_*.cache"))
		if err != nil {
			continue
		}
		for _, path := range matches {
			if os.Remove(path) == nil {
				dropped++
			}
		}
	}
	if dropped > 0 {
		slog.Debug("cache invalidated", "tags", tags, "files", dropped)
	}
	return dropped
}

// Fetch performs the request, honouring the cache, and retries what is worth retrying.
func Fetch(request Request) (string, error) {
	if request.Method == "" {
		request.Method = http.MethodGet
	}
	if request.Tag == "" {
		request.Tag = "raw"
	}
	if request.Timeout == 0 {
		request.Timeout = 30 * time.Second
	}
	if request.Retries == 0 {
		request.Retries = 3
	}

	if body, ok := readCache(request.URL, request.Tag, request.TTL); ok {
		bump(&stats.CacheHits)
		slog.Debug("http cache hit", "url", request.URL, "tag", request.Tag,
			"bytes", len(body))
		return body, nil
	}

	if Frozen {
		return "", &FrozenMiss{URL: request.URL, Tag: request.Tag}
	}

	var lastErr error
	for attempt := 1; attempt <= request.Retries; attempt++ {
		body, status, err := once(request)
		if err == nil {
			slog.Debug("http ok", "url", request.URL, "tag", request.Tag, "status", status,
				"bytes", len(body))
			if request.TTL > 0 || request.Store {
				writeCache(request.URL, request.Tag, body)
			}
			return body, nil
		}

		var limited *RateLimited
		if errors.As(err, &limited) {
			return "", err
		}
		var httpErr *Error
		if errors.As(err, &httpErr) &&
			(httpErr.Status == 401 || httpErr.Status == 403 || httpErr.Status == 404) {
			return "", err
		}
		lastErr = err
		slog.Warn("http failure", "url", request.URL, "tag", request.Tag,
			"attempt", attempt, "reason", err.Error())
		time.Sleep(time.Duration(attempt) * 600 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("unreachable: %s", request.URL)
	}
	return "", lastErr
}

func once(request Request) (string, int, error) {
	bump(&stats.Requests)
	req, err := http.NewRequest(request.Method, request.URL, bodyReader(request.Body))
	if err != nil {
		return "", 0, err
	}
	for key, value := range request.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	ctxClient := &http.Client{Timeout: request.Timeout}
	resp, err := ctxClient.Do(req)
	if err != nil {
		bump(&stats.Errors)
		return "", 0, err
	}
	defer resp.Body.Close()

	body, err := decode(resp)
	if err != nil {
		bump(&stats.Errors)
		return "", resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		bump(&stats.Errors)
		if resp.StatusCode == 429 {
			return "", resp.StatusCode, &RateLimited{
				URL: request.URL, RetryAfter: resp.Header.Get("Retry-After")}
		}
		return "", resp.StatusCode, &Error{Status: resp.StatusCode, URL: request.URL,
			Body: truncate(body, 400)}
	}
	return body, resp.StatusCode, nil
}

func decode(resp *http.Response) (string, error) {
	reader := io.Reader(resp.Body)
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		unzipped, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", err
		}
		defer unzipped.Close()
		reader = unzipped
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func bodyReader(body []byte) io.Reader {
	if len(body) == 0 {
		return nil
	}
	return strings.NewReader(string(body))
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

// GetJSON fetches and unmarshals into target.
func GetJSON(request Request, target any) error {
	body, err := Fetch(request)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), target)
}

// PostForm is the shape the identity provider wants: form-encoded, JSON back.
func PostForm(rawURL string, form map[string]string, target any) error {
	values := url.Values{}
	for key, value := range form {
		values.Set(key, value)
	}
	body, err := Fetch(Request{
		URL:    rawURL,
		Method: http.MethodPost,
		Body:   []byte(values.Encode()),
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
			"User-Agent":   config.APIHeaders()["User-Agent"],
		},
		Tag: "raw",
	})
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(body), target)
}
