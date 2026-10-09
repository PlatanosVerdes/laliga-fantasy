// Package services is the registry of the clocks the server runs on: how often it probes the
// league, rebuilds the world, follows a live match and keeps the push channel awake.
//
// Each clock resolves the same way: an override saved from the page, else the environment
// variable FANTASY_<KEY>_INTERVAL, else the default. Jobs ask for their interval every cycle,
// so a change from the page applies without a restart.
package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Where an interval came from.
const (
	SourceDefault = "default"
	SourceFlag    = "flag"
	SourceEnv     = "env"
	SourceUI      = "ui"
)

// Job is one clock and what the page says about it.
type Job struct {
	Key         string
	Label       string
	Description string
	Default     time.Duration
	Min         time.Duration
	Max         time.Duration
	// FromFlag marks a Default that came from the command line rather than the code.
	FromFlag bool
}

// EnvName is the variable that overrides the job's default.
func (j Job) EnvName() string {
	return "FANTASY_" + strings.ToUpper(j.Key) + "_INTERVAL"
}

// Entry is a job as the API answers it.
type Entry struct {
	Key            string  `json:"key"`
	Label          string  `json:"label"`
	Description    string  `json:"description"`
	Env            string  `json:"env"`
	Seconds        float64 `json:"seconds"`
	Interval       string  `json:"interval"`
	Source         string  `json:"source"`
	DefaultSeconds float64 `json:"default_seconds"`
	Default        string  `json:"default"`
	MinSeconds     float64 `json:"min_seconds"`
	Min            string  `json:"min"`
	MaxSeconds     float64 `json:"max_seconds"`
	Max            string  `json:"max"`
	LastRun        *string `json:"last_run"`
	NextRun        *string `json:"next_run"`
}

// Status reports when a job last ran and when it runs next; zero times are unknown.
type Status func() (last, next time.Time)

type Registry struct {
	mu        sync.RWMutex
	path      string
	jobs      []Job
	env       map[string]time.Duration
	overrides map[string]time.Duration
	status    map[string]Status
	listeners []func(key string)
}

var (
	ErrUnknown = errors.New("servicio desconocido")
	ErrBounds  = errors.New("fuera de los limites")
	ErrInvalid = errors.New("intervalo no valido")
)

// New builds the registry from the jobs, the environment and the overrides file at path. A
// variable or a saved override that does not parse or falls outside the bounds is ignored with
// a warning: a typo must not stop the server, and the default is always a safe answer.
func New(path string, jobs ...Job) *Registry {
	r := &Registry{path: path, jobs: jobs, env: map[string]time.Duration{},
		overrides: map[string]time.Duration{}, status: map[string]Status{}}
	for _, job := range jobs {
		raw := strings.TrimSpace(os.Getenv(job.EnvName()))
		if raw == "" {
			continue
		}
		value, err := ParseInterval(raw)
		if err == nil {
			err = job.check(value)
		}
		if err != nil {
			slog.Warn("interval variable ignored", "env", job.EnvName(), "value", raw,
				"reason", err.Error())
			continue
		}
		r.env[job.Key] = value
	}
	for key, value := range r.load() {
		job, ok := r.job(key)
		if !ok || job.check(value) != nil {
			slog.Warn("saved interval ignored", "key", key, "value", value.String())
			continue
		}
		r.overrides[key] = value
	}
	return r
}

func (j Job) check(value time.Duration) error {
	if value < j.Min || value > j.Max {
		return fmt.Errorf("%w: %s esta entre %s y %s", ErrBounds, j.Label, Format(j.Min),
			Format(j.Max))
	}
	return nil
}

func (r *Registry) job(key string) (Job, bool) {
	for _, job := range r.jobs {
		if job.Key == key {
			return job, true
		}
	}
	return Job{}, false
}

// Get is the interval a job should use right now.
func (r *Registry) Get(key string) time.Duration {
	value, _ := r.resolve(key)
	return value
}

func (r *Registry) resolve(key string) (time.Duration, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if value, ok := r.overrides[key]; ok {
		return value, SourceUI
	}
	if value, ok := r.env[key]; ok {
		return value, SourceEnv
	}
	job, _ := r.job(key)
	if job.FromFlag {
		return job.Default, SourceFlag
	}
	return job.Default, SourceDefault
}

// Set saves an override from the page and applies it.
func (r *Registry) Set(key string, value time.Duration) error {
	job, ok := r.job(key)
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknown, key)
	}
	if err := job.check(value); err != nil {
		return err
	}
	return r.change(key, func() { r.overrides[key] = value })
}

// Reset drops the override, so the variable or the default applies again.
func (r *Registry) Reset(key string) error {
	if _, ok := r.job(key); !ok {
		return fmt.Errorf("%w: %s", ErrUnknown, key)
	}
	return r.change(key, func() { delete(r.overrides, key) })
}

func (r *Registry) change(key string, apply func()) error {
	r.mu.Lock()
	before := r.overrides[key]
	_, had := r.overrides[key]
	apply()
	if err := r.save(); err != nil {
		if had {
			r.overrides[key] = before
		} else {
			delete(r.overrides, key)
		}
		r.mu.Unlock()
		return err
	}
	listeners := append([]func(string){}, r.listeners...)
	r.mu.Unlock()
	for _, listener := range listeners {
		listener(key)
	}
	return nil
}

// OnChange is called after an interval changes, so a job sleeping on the old one can replan.
func (r *Registry) OnChange(listener func(key string)) {
	r.mu.Lock()
	r.listeners = append(r.listeners, listener)
	r.mu.Unlock()
}

// Track hangs a job's last and next run on its entry.
func (r *Registry) Track(key string, status Status) {
	r.mu.Lock()
	r.status[key] = status
	r.mu.Unlock()
}

// List is every job, in the order they were registered.
func (r *Registry) List() []Entry {
	entries := make([]Entry, 0, len(r.jobs))
	for _, job := range r.jobs {
		value, source := r.resolve(job.Key)
		entry := Entry{Key: job.Key, Label: job.Label, Description: job.Description,
			Env: job.EnvName(), Seconds: value.Seconds(), Interval: Format(value), Source: source,
			DefaultSeconds: job.Default.Seconds(), Default: Format(job.Default),
			MinSeconds: job.Min.Seconds(), Min: Format(job.Min),
			MaxSeconds: job.Max.Seconds(), Max: Format(job.Max)}
		r.mu.RLock()
		status := r.status[job.Key]
		r.mu.RUnlock()
		if status != nil {
			last, next := status()
			entry.LastRun, entry.NextRun = stamp(last), stamp(next)
		}
		entries = append(entries, entry)
	}
	return entries
}

func stamp(when time.Time) *string {
	if when.IsZero() {
		return nil
	}
	text := when.UTC().Format(time.RFC3339)
	return &text
}

func (r *Registry) load() map[string]time.Duration {
	out := map[string]time.Duration{}
	if r.path == "" {
		return out
	}
	body, err := os.ReadFile(r.path)
	if err != nil {
		return out
	}
	var saved map[string]string
	if err := json.Unmarshal(body, &saved); err != nil {
		slog.Warn("services file unreadable", "path", r.path, "reason", err.Error())
		return out
	}
	for key, raw := range saved {
		if value, err := ParseInterval(raw); err == nil {
			out[key] = value
		}
	}
	return out
}

func (r *Registry) save() error {
	if r.path == "" {
		return nil
	}
	saved := make(map[string]string, len(r.overrides))
	keys := make([]string, 0, len(r.overrides))
	for key := range r.overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		saved[key] = Format(r.overrides[key])
	}
	blob, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(r.path, blob, 0o600)
}

// ParseInterval accepts a Go duration ("90s", "5m", "1h30m") or a bare number of seconds.
func ParseInterval(value string) (time.Duration, error) {
	text := strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(text); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	if text == "" {
		return 0, fmt.Errorf("%w: vacio", ErrInvalid)
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("%w: %q (usa 90s, 5m o 1h)", ErrInvalid, value)
	}
	return parsed, nil
}

// Format writes a duration the way it is typed: "2m", "1h30m", "20s".
func Format(value time.Duration) string {
	text := value.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}
