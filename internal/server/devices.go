package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"
)

// deviceOf names the browser and system in the user agent the way a sessions list does,
// "iPhone · Safari": the full string means nothing to the person reading it.
func deviceOf(request *http.Request) string {
	agent := request.UserAgent()
	system := firstMatch(agent, [][2]string{{"iPhone", "iPhone"}, {"iPad", "iPad"},
		{"Android", "Android"}, {"CrOS", "ChromeOS"}, {"Macintosh", "Mac"},
		{"Windows", "Windows"}, {"Linux", "Linux"}})
	// Order matters: Edge and Chrome on iOS also say Safari, and Edge also says Chrome.
	browser := firstMatch(agent, [][2]string{{"Edg", "Edge"}, {"OPR", "Opera"},
		{"Firefox", "Firefox"}, {"FxiOS", "Firefox"}, {"CriOS", "Chrome"},
		{"Chrome", "Chrome"}, {"Safari", "Safari"}})
	switch {
	case system != "" && browser != "":
		return system + " · " + browser
	case system != "":
		return system
	case browser != "":
		return browser
	}
	return ""
}

func firstMatch(agent string, names [][2]string) string {
	for _, name := range names {
		if strings.Contains(agent, name[0]) {
			return name[1]
		}
	}
	return ""
}

var tailnet = mustCIDR("100.64.0.0/10")

func mustCIDR(block string) *net.IPNet {
	_, network, err := net.ParseCIDR(block)
	if err != nil {
		panic(err)
	}
	return network
}

// placeOf is where a request came from. Only the Access door is reached through Cloudflare,
// so only there are its location headers trusted; the city ones arrive only when the zone's
// "Add visitor location headers" managed transform is on.
func placeOf(request *http.Request) place {
	if doorOf(request) == "access" {
		return place{City: request.Header.Get("Cf-Ipcity"), Region: request.Header.Get("Cf-Region"),
			Country: strings.ToUpper(request.Header.Get("Cf-Ipcountry"))}
	}
	ip := net.ParseIP(clientIP(request))
	switch {
	case ip == nil:
		return place{}
	case tailnet.Contains(ip):
		return place{Network: "tailnet"}
	case ip.IsPrivate() || ip.IsLoopback():
		return place{Network: "lan"}
	}
	return place{}
}

// A session is named by the start of its key: the key is already a hash of the cookie, so
// showing it signs nobody in.
func sessionID(key string) string { return key[:16] }

func (s *Server) devices(writer http.ResponseWriter, request *http.Request) {
	who, _ := visitorOf(request)
	sessions := s.doors().sessions
	if request.Method == http.MethodPost {
		var body struct {
			Close string `json:"close"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		closed := 0
		for _, open := range sessions.listUser(who.User) {
			if open.key == who.key {
				continue
			}
			if body.Close != "" && sessionID(open.key) == body.Close {
				sessions.close(open.key)
				closed++
			}
		}
		if closed == 0 {
			s.json(writer, http.StatusNotFound, map[string]any{"error": "esa sesion ya no existe"})
			return
		}
		s.json(writer, http.StatusOK, map[string]any{"closed": closed})
		return
	}
	list := []map[string]any{}
	for _, open := range sessions.listUser(who.User) {
		list = append(list, map[string]any{"id": sessionID(open.key), "device": open.Device,
			"place": open.Place, "seen": time.Unix(open.Seen, 0).UTC().Format(time.RFC3339),
			"created": time.Unix(open.Created, 0).UTC().Format(time.RFC3339),
			"current": open.key == who.key})
	}
	s.json(writer, http.StatusOK, map[string]any{"count": len(list), "devices": list})
}

func (s *Server) devicesPage(writer http.ResponseWriter, request *http.Request) {
	http.Redirect(writer, request, "/?abrir=dispositivos", http.StatusSeeOther)
}
