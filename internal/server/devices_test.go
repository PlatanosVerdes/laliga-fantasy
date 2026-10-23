package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeviceNamesTheSystemAndTheBrowser(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": "iPhone · Safari",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0 Mobile/15E148 Safari/604.1":  "iPhone · Chrome",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0":     "Mac · Edge",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36":                   "Android · Chrome",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:132.0) Gecko/20100101 Firefox/132.0":                                                        "Windows · Firefox",
		"curl/8.7.1": "",
	}
	for agent, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "http://x/", nil)
		request.Header.Set("User-Agent", agent)
		if got := deviceOf(request); got != want {
			t.Errorf("%q: got %q, want %q", agent, got, want)
		}
	}
}

func TestPlaceTrustsCloudflareOnlyThroughAccess(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://x/", nil)
	request.Header.Set("Cf-Ipcountry", "es")
	request.Header.Set("Cf-Ipcity", "Palma")
	request.Header.Set("Cf-Region", "Balearic Islands")
	request.RemoteAddr = "100.101.102.103:5000"
	if got := placeOf(request); got != (place{Network: "tailnet"}) {
		t.Errorf("private door: %+v", got)
	}
	request.RemoteAddr = "192.168.1.20:5000"
	if got := placeOf(request); got != (place{Network: "lan"}) {
		t.Errorf("LAN: %+v", got)
	}
	want := place{City: "Palma", Region: "Balearic Islands", Country: "ES"}
	if got := placeOf(withDoor(request, "access")); got != want {
		t.Errorf("through Access: %+v", got)
	}
}

func TestDevicesListsAndClosesOnlyTheVisitorsSessions(t *testing.T) {
	server := New(nil, Options{Allow: "*"})
	here := ownerSession(t, server)
	phone := sessionFor(t, server, "user-me")
	stranger := sessionFor(t, server, "user-other")
	var listed struct {
		Devices []struct {
			ID      string `json:"id"`
			Current bool   `json:"current"`
		} `json:"devices"`
	}
	ids := func() map[bool]string {
		answer := visit(server.Handler(), http.MethodGet, "/api/devices", "", here)
		if err := json.Unmarshal(answer.Body.Bytes(), &listed); err != nil || len(listed.Devices) != 2 {
			t.Fatalf("devices: %d %s", answer.Code, answer.Body)
		}
		out := map[bool]string{}
		for _, device := range listed.Devices {
			out[device.Current] = device.ID
		}
		return out
	}
	mine := ids()
	strangerID := sessionID(hashOf(stranger.Value))
	if answer := visit(server.Handler(), http.MethodPost, "/api/devices", `{"close":"`+strangerID+`"}`, here); answer.Code != http.StatusNotFound {
		t.Errorf("closed somebody else's session: %d", answer.Code)
	}
	if answer := visit(server.Handler(), http.MethodPost, "/api/devices", `{"close":"`+mine[true]+`"}`, here); answer.Code != http.StatusNotFound {
		t.Errorf("closed the current session from the list: %d", answer.Code)
	}
	if answer := visit(server.Handler(), http.MethodPost, "/api/devices", `{"close":"`+mine[false]+`"}`, here); answer.Code != http.StatusOK {
		t.Fatalf("closing the phone: %d %s", answer.Code, answer.Body)
	}
	if _, ok := server.doors().sessions.find(phone.Value); ok {
		t.Error("the phone is still signed in")
	}
	if _, ok := server.doors().sessions.find(stranger.Value); !ok {
		t.Error("the other user was signed out")
	}
}

func withDoor(request *http.Request, door string) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), doorKey{}, door))
}

func TestARequestThatSaysNothingKeepsTheKnownDevice(t *testing.T) {
	store := newSessionStore("")
	_, who, err := store.open("user-me", "")
	if err != nil {
		t.Fatal(err)
	}
	store.note(who.key, "iPhone · Safari", place{Country: "ES"})
	store.note(who.key, "", place{})
	got := store.listUser("user-me")[0]
	if got.Device != "iPhone · Safari" || got.Place.Country != "ES" {
		t.Errorf("an anonymous request erased the device: %+v", got)
	}
}
