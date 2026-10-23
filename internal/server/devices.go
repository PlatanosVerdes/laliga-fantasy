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
func placeOf(request *http.Request) string {
	if doorOf(request) == "access" {
		country := strings.ToUpper(request.Header.Get("Cf-Ipcountry"))
		where := request.Header.Get("Cf-Ipcity")
		if where == "" {
			where = request.Header.Get("Cf-Region")
		}
		if where == "" {
			where = country
		}
		if flag := flagOf(country); flag != "" {
			return strings.TrimSpace(flag + " " + where)
		}
		return where
	}
	ip := net.ParseIP(clientIP(request))
	switch {
	case ip == nil:
		return ""
	case tailnet.Contains(ip):
		return "Tailnet"
	case ip.IsPrivate() || ip.IsLoopback():
		return "Red local"
	}
	return ""
}

// flagOf turns an ISO country code into its flag: two regional indicator symbols.
func flagOf(code string) string {
	if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
		return ""
	}
	return string([]rune{rune(code[0]) - 'A' + 0x1F1E6, rune(code[1]) - 'A' + 0x1F1E6})
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
		for _, open := range sessions.listUser(who.User) {
			if body.Close != "" && sessionID(open.key) == body.Close && open.key != who.key {
				sessions.close(open.key)
				s.json(writer, http.StatusOK, map[string]any{"ok": true})
				return
			}
		}
		s.json(writer, http.StatusNotFound, map[string]any{"error": "esa sesion ya no existe"})
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
