package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PlatanosVerdes/laliga-fantasy/internal/auth"
	"github.com/PlatanosVerdes/laliga-fantasy/internal/config"
)

// Another site can post a form here from your browser, never JSON: only JSON may write.
func TestOnlyJSONWritesReachTheAPI(t *testing.T) {
	reached := false
	handler := sameSiteOnly(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for _, c := range []struct {
		path, kind string
		want       bool
	}{
		{"/api/raid", "text/plain", false},
		{"/api/raid", "application/x-www-form-urlencoded", false},
		{"/api/raid", "application/json", true},
		{"/api/usage", "text/plain", true},
	} {
		reached = false
		request := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", c.kind)
		handler.ServeHTTP(httptest.NewRecorder(), request)
		if reached != c.want {
			t.Errorf("POST %s as %s reached the handler: %v, want %v", c.path, c.kind, reached, c.want)
		}
	}
}

// A working session is not overwritten by a stray paste.
func TestAWorkingSessionIsNotReplacedByAccident(t *testing.T) {
	config.TokenFile = filepath.Join(t.TempDir(), "tokens.json")
	if err := auth.Save(&auth.Tokens{AccessToken: "good", RefreshToken: "good"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	request := httptest.NewRequest(http.MethodPost, "/api/session",
		strings.NewReader(`{"paste":"{\"refresh_token\":\"x\"}"}`))
	request = request.WithContext(context.WithValue(request.Context(), visitorKey{}, visitor{User: "user-me"}))
	recorder := httptest.NewRecorder()
	s.session(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Errorf("status %d, want 409", recorder.Code)
	}
	if tokens, _ := auth.Load(); tokens == nil || tokens.RefreshToken != "good" {
		t.Errorf("the working session was replaced: %+v", tokens)
	}
}
