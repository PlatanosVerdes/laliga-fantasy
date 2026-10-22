package httpxtest_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/httpx/httpxtest"
)

func TestTheFakeRecordsTheBearerMethodPathAndBodyOfEveryRequest(t *testing.T) {
	fake := httpxtest.New(t)
	fake.Answer("POST /api/v3/league/7/market/9/bid", http.StatusOK, `{"ok":true}`)
	t.Cleanup(httpx.SetTransport(fake))

	body, err := httpx.Fetch(httpx.Request{
		URL:    "https://fantasy-api.llt-services.com/api/v3/league/7/market/9/bid?x-lang=es",
		Method: http.MethodPost, Body: []byte(`{"money":100}`), Retries: 1,
		Headers: map[string]string{"Authorization": "Bearer A"},
	})
	if err != nil || body != `{"ok":true}` {
		t.Fatalf("body=%q err=%v", body, err)
	}
	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls: %+v", calls)
	}
	got := calls[0]
	if got.Authorization != "Bearer A" || got.Method != http.MethodPost ||
		got.Host != "fantasy-api.llt-services.com" || got.Path != "/api/v3/league/7/market/9/bid" ||
		string(got.Body) != `{"money":100}` || got.Query != "x-lang=es" {
		t.Errorf("recorded %+v", got)
	}
}

func TestAnUnknownRouteIsA404(t *testing.T) {
	fake := httpxtest.New(t)
	t.Cleanup(httpx.SetTransport(fake))
	_, err := httpx.Fetch(httpx.Request{URL: "https://fantasy-api.llt-services.com/nowhere"})
	var refused *httpx.Error
	if !errors.As(err, &refused) || refused.Status != http.StatusNotFound {
		t.Fatalf("err=%v", err)
	}
}
