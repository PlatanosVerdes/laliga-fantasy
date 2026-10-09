package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pollJob() Job {
	return Job{Key: "poll", Label: "Sondeo", Default: 2 * time.Minute,
		Min: 30 * time.Second, Max: 6 * time.Hour}
}

func source(r *Registry, key string) string {
	for _, entry := range r.List() {
		if entry.Key == key {
			return entry.Source
		}
	}
	return ""
}

func TestDefaultThenEnvThenPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")

	r := New(path, pollJob())
	if got := r.Get("poll"); got != 2*time.Minute || source(r, "poll") != SourceDefault {
		t.Fatalf("sin nada, el valor de siempre: %v %s", got, source(r, "poll"))
	}

	t.Setenv("FANTASY_POLL_INTERVAL", "5m")
	r = New(path, pollJob())
	if got := r.Get("poll"); got != 5*time.Minute || source(r, "poll") != SourceEnv {
		t.Fatalf("la variable manda sobre el defecto: %v %s", got, source(r, "poll"))
	}

	if err := r.Set("poll", 90*time.Second); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("poll"); got != 90*time.Second || source(r, "poll") != SourceUI {
		t.Fatalf("el panel manda sobre la variable: %v %s", got, source(r, "poll"))
	}
	if entry := r.List()[0]; entry.Fallback != "5m" || entry.FallbackSource != SourceEnv {
		t.Fatalf("sin el panel quedaria la variable: %+v", entry)
	}

	if err := r.Reset("poll"); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("poll"); got != 5*time.Minute || source(r, "poll") != SourceEnv {
		t.Fatalf("por defecto vuelve a la variable, no al codigo: %v %s", got, source(r, "poll"))
	}
}

func TestBareSecondsInTheEnvironment(t *testing.T) {
	t.Setenv("FANTASY_POLL_INTERVAL", "3600")
	if got := New("", pollJob()).Get("poll"); got != time.Hour {
		t.Fatalf("3600 son segundos: %v", got)
	}
}

func TestTheFlagSitsBelowTheEnvironment(t *testing.T) {
	job := pollJob()
	job.Default, job.FromFlag = time.Hour, true
	r := New("", job)
	if got := r.Get("poll"); got != time.Hour || source(r, "poll") != SourceFlag {
		t.Fatalf("sin variable, la bandera: %v %s", got, source(r, "poll"))
	}
	t.Setenv("FANTASY_POLL_INTERVAL", "10m")
	r = New("", job)
	if got := r.Get("poll"); got != 10*time.Minute {
		t.Fatalf("la variable gana a la bandera: %v", got)
	}
}

func TestAVariableOutOfBoundsIsIgnored(t *testing.T) {
	for _, value := range []string{"1s", "24h", "pronto"} {
		t.Setenv("FANTASY_POLL_INTERVAL", value)
		if got := New("", pollJob()).Get("poll"); got != 2*time.Minute {
			t.Errorf("%q no deberia aplicarse: %v", value, got)
		}
	}
}

func TestBounds(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "services.json"), pollJob())
	for _, value := range []time.Duration{0, -time.Minute, 29 * time.Second, 6*time.Hour + 1} {
		if err := r.Set("poll", value); !errors.Is(err, ErrBounds) {
			t.Errorf("%v deberia rechazarse por limites: %v", value, err)
		}
	}
	for _, value := range []time.Duration{30 * time.Second, 6 * time.Hour} {
		if err := r.Set("poll", value); err != nil {
			t.Errorf("%v esta dentro: %v", value, err)
		}
	}
	if err := r.Set("nada", time.Minute); !errors.Is(err, ErrUnknown) {
		t.Errorf("un servicio que no existe: %v", err)
	}
	if err := r.Reset("nada"); !errors.Is(err, ErrUnknown) {
		t.Errorf("un servicio que no existe: %v", err)
	}
}

func TestOverridesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "services.json")
	r := New(path, pollJob())
	if err := r.Set("poll", 45*time.Second); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("el fichero va en 0600: %v", info.Mode().Perm())
	}
	if got := New(path, pollJob()).Get("poll"); got != 45*time.Second {
		t.Fatalf("tras reiniciar se pierde el cambio: %v", got)
	}

	if err := r.Reset("poll"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if strings.Contains(string(body), "poll") {
		t.Errorf("por defecto borra la entrada: %s", body)
	}
	if got := New(path, pollJob()).Get("poll"); got != 2*time.Minute {
		t.Fatalf("tras restablecer y reiniciar, el defecto: %v", got)
	}
}

func TestASavedValueOutOfBoundsIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"poll":"1s","otro":"5m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := New(path, pollJob()).Get("poll"); got != 2*time.Minute {
		t.Fatalf("un valor guardado fuera de limites no se aplica: %v", got)
	}
}

func TestAChangeIsAnnounced(t *testing.T) {
	r := New(filepath.Join(t.TempDir(), "services.json"), pollJob())
	var heard []string
	r.OnChange(func(key string) { heard = append(heard, key+"="+r.Get(key).String()) })
	_ = r.Set("poll", time.Minute)
	_ = r.Set("poll", time.Second)
	_ = r.Reset("poll")
	if strings.Join(heard, ",") != "poll=1m0s,poll=2m0s" {
		t.Fatalf("se avisa de cada cambio aplicado, y solo de esos: %v", heard)
	}
}

func TestStatusRidesOnTheEntry(t *testing.T) {
	r := New("", pollJob())
	last := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	r.Track("poll", func() (time.Time, time.Time) { return last, time.Time{} })
	entry := r.List()[0]
	if entry.LastRun == nil || *entry.LastRun != "2026-10-09T10:00:00Z" || entry.NextRun != nil {
		t.Fatalf("ultima y proxima: %+v", entry)
	}
	if entry.Env != "FANTASY_POLL_INTERVAL" || entry.Interval != "2m" || entry.Default != "2m" {
		t.Fatalf("entrada: %+v", entry)
	}
}

func TestParseAndFormat(t *testing.T) {
	cases := map[string]time.Duration{"90s": 90 * time.Second, "5m": 5 * time.Minute,
		"1h": time.Hour, "120": 2 * time.Minute, " 1h30m ": 90 * time.Minute}
	for text, want := range cases {
		if got, err := ParseInterval(text); err != nil || got != want {
			t.Errorf("%q: %v %v", text, got, err)
		}
	}
	for _, text := range []string{"", "pronto", "5 minutos"} {
		if _, err := ParseInterval(text); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q deberia ser invalido: %v", text, err)
		}
	}
	formats := map[time.Duration]string{20 * time.Second: "20s", 2 * time.Minute: "2m",
		90 * time.Second: "1m30s", time.Hour: "1h", 90 * time.Minute: "1h30m"}
	for value, want := range formats {
		if got := Format(value); got != want {
			t.Errorf("%v: %q, esperaba %q", value, got, want)
		}
	}
}

func TestAnEnvOnlyClockRefusesThePage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"heartbeat":"40s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	job := Job{Key: "heartbeat", Label: "Latido", Default: 20 * time.Second,
		Min: 5 * time.Second, Max: time.Minute, EnvOnly: true}
	t.Setenv("FANTASY_HEARTBEAT_INTERVAL", "30s")
	r := New(path, job)
	if got := r.Get("heartbeat"); got != 30*time.Second {
		t.Fatalf("un valor guardado no aplica a un reloj solo de entorno: %v", got)
	}
	if err := r.Set("heartbeat", 10*time.Second); !errors.Is(err, ErrEnvOnly) {
		t.Errorf("el panel no lo cambia: %v", err)
	}
	if err := r.Reset("heartbeat"); !errors.Is(err, ErrEnvOnly) {
		t.Errorf("ni lo restablece: %v", err)
	}
	if entry := r.List()[0]; entry.Editable || entry.Source != SourceEnv {
		t.Errorf("se lista como no editable, desde la variable: %+v", entry)
	}
}

func TestValuesRoundToWholeMinutes(t *testing.T) {
	job := pollJob()
	job.Min, job.Unit = time.Minute, time.Minute
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"poll":"1m30s"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := New(path, job)
	if got := r.Get("poll"); got != 2*time.Minute {
		t.Fatalf("1m30s guardado se redondea al minuto: %v", got)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `"2m"`) {
		t.Errorf("el fichero se reescribe redondeado: %s", body)
	}

	t.Setenv("FANTASY_POLL_INTERVAL", "200s")
	r = New("", job)
	if got := r.Get("poll"); got != 3*time.Minute {
		t.Fatalf("la variable tambien se redondea: %v", got)
	}
	if err := r.Set("poll", 100*time.Second); err != nil || r.Get("poll") != 2*time.Minute {
		t.Fatalf("y lo que llega del panel: %v %v", r.Get("poll"), err)
	}
}
