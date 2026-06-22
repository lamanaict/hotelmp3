// HotelMP3 — AV Control System Backend (Go)
// Complete rewrite based on Resource design documents
// Spatial UI + HUD UI architecture with real-time WebSocket sync
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ─── Config ────────────────────────────────────────────────────

var (
	port      = flag.Int("port", 8000, "Server port")
	noBrowser = flag.Bool("no-browser", false, "Don't open browser")
	staticDir string
	appDir    string
)

// ─── Zone / Room State ─────────────────────────────────────────

type ZoneState struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"` // guest_room, lobby, conference, restaurant
	Power            bool                   `json:"power"`
	SourceID         string                 `json:"source_id"`
	AudioVolume      int                    `json:"audio_volume"`
	AudioMuted       bool                   `json:"audio_muted"`
	MicActive        bool                   `json:"mic_active"`
	MicVolume        int                    `json:"mic_volume"`
	Group            string                 `json:"group,omitempty"`
	Online           bool                   `json:"online"`
	DisplayDevice    map[string]interface{} `json:"display_device,omitempty"`
	SpeakerDevice    map[string]interface{} `json:"speaker_device,omitempty"`
	MicDevice        map[string]interface{} `json:"mic_device,omitempty"`
	SourceDevice     map[string]interface{} `json:"source_device,omitempty"`
	EqBass           int                    `json:"eq_bass"`
	EqMid            int                    `json:"eq_mid"`
	EqTreble         int                    `json:"eq_treble"`
	Balance          int                    `json:"balance"`
	AudioPreset      string                 `json:"audio_preset"`
	// Media
	MediaSource      string        `json:"media_source"`
	MediaURL         string        `json:"media_url"`
	MediaPlaylist    []interface{} `json:"media_playlist"`
	MediaCurrentIdx  int           `json:"media_current_idx"`
	MediaPlaying     bool          `json:"media_playing"`
	MediaPosition    int           `json:"media_position"`
	MediaDuration    int           `json:"media_duration"`
	MediaLoop        bool          `json:"media_loop"`
	MediaShuffle     bool          `json:"media_shuffle"`
	// Screen Share
	ScreenShareActive bool        `json:"screen_share_active"`
	ScreenShareMode  string        `json:"screen_share_mode"`
	ScreenShareTarget interface{}  `json:"screen_share_target,omitempty"`
	// Remote
	RemoteConnection map[string]interface{} `json:"remote_connection,omitempty"`
	// HUD data
	HUDLatency       int                    `json:"hud_latency_ms"`
	HUDEncryption    string                 `json:"hud_encryption"`
	HUDHealthData    map[string]interface{} `json:"hud_health_data,omitempty"`
}

type SourceInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// ─── App State ─────────────────────────────────────────────────

type AppState struct {
	mu                sync.RWMutex
	Sources           map[string]SourceInfo
	Zones             map[string]*ZoneState
	Clients           []*websocket.Conn
	GroupNames        map[string]string
	RemoteConnections map[string]map[string]interface{}
	// Media cache
	MediaCache        *MediaCache
	// Scan state
	ScanResults       []map[string]interface{}
	ScanInProgress    bool
	ScanProgressMsg   string
	ScanProgressPct   int
	// Display
	WirelessDisplays  []map[string]interface{}
	DisplayGroups     map[string]map[string]interface{}
}

var state *AppState

func NewAppState() *AppState {
	s := &AppState{
		Sources: map[string]SourceInfo{
			"source_1": {ID: "source_1", Name: "Source 1 (Sync)", Icon: "📺", Color: "#3b82f6"},
			"source_2": {ID: "source_2", Name: "Source 2 (Movie)", Icon: "🎬", Color: "#8b5cf6"},
			"source_3": {ID: "source_3", Name: "Source 3 (Live Sports)", Icon: "⚽", Color: "#10b981"},
			"source_4": {ID: "source_4", Name: "Source 4 (Presentation)", Icon: "📊", Color: "#f59e0b"},
		},
		Zones:             make(map[string]*ZoneState),
		Clients:           make([]*websocket.Conn, 0),
		GroupNames:        make(map[string]string),
		RemoteConnections: make(map[string]map[string]interface{}),
		MediaCache:        &MediaCache{},
		DisplayGroups:     make(map[string]map[string]interface{}),
	}

	// Initialize zones
	zones := []struct {
		id, name, zoneType, group string
	}{
		{"zone_1", "Guest Room 101", "guest_room", "A"},
		{"zone_2", "Guest Room 102", "guest_room", "A"},
		{"zone_3", "Guest Room 103", "guest_room", ""},
		{"zone_4", "Main Lobby", "lobby", ""},
		{"zone_5", "Conference Room A", "conference", ""},
		{"zone_6", "Restaurant", "restaurant", ""},
	}

	for _, z := range zones {
		s.Zones[z.id] = &ZoneState{
			ID:              z.id,
			Name:            z.name,
			Type:            z.zoneType,
			Power:           false,
			SourceID:        "source_1",
			AudioVolume:     50,
			AudioMuted:      false,
			MicActive:       true,
			MicVolume:       75,
			Group:           z.group,
			Online:          true,
			EqBass:          0,
			EqMid:           0,
			EqTreble:        0,
			Balance:         0,
			AudioPreset:     "flat",
			MediaSource:     "none",
			MediaCurrentIdx: -1,
			HUDEncryption:   "AES-256",
		}
	}
	return s
}

func (s *AppState) FullState() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	zones := make(map[string]interface{})
	for k, v := range s.Zones {
		zones[k] = v
	}
	return map[string]interface{}{
		"type":               "full_state",
		"sources":            s.Sources,
		"zones":              zones,
		"remote_connections": s.RemoteConnections,
		"group_names":        s.GroupNames,
		"channels":           lineup.GetAll(),
		"channel_groups":     lineup.GetGroups(),
	}
}

func (s *AppState) Broadcast(msg map[string]interface{}) {
	s.mu.RLock()
	clients := make([]*websocket.Conn, len(s.Clients))
	copy(clients, s.Clients)
	s.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Broadcast marshal error: %v", err)
		return
	}

	for _, c := range clients {
		c.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Broadcast write error: %v", err)
			s.RemoveClient(c)
		}
	}
}

func (s *AppState) AddClient(c *websocket.Conn) {
	s.mu.Lock()
	s.Clients = append(s.Clients, c)
	s.mu.Unlock()
}

func (s *AppState) RemoveClient(c *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, client := range s.Clients {
		if client == c {
			s.Clients = append(s.Clients[:i], s.Clients[i+1:]...)
			break
		}
	}
}

// ─── WebSocket Upgrader ────────────────────────────────────────

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

// ─── Helpers ───────────────────────────────────────────────────

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

func getAllLocalIPs() []string {
	var ips []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return []string{"127.0.0.1"}
	}
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil {
				ips = append(ips, ipNet.IP.String())
			}
		}
	}
	if len(ips) == 0 {
		ips = append(ips, "127.0.0.1")
	}
	return ips
}

func formatSize(sz int64) string {
	if sz < 1024 {
		return fmt.Sprintf("%d B", sz)
	}
	if sz < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(sz)/1024)
	}
	if sz < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(sz)/(1024*1024))
	}
	return fmt.Sprintf("%.1f GB", float64(sz)/(1024*1024*1024))
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func updateZoneFromData(zone *ZoneState, data map[string]interface{}) {
	if v, ok := data["power"].(bool); ok {
		zone.Power = v
	}
	if v, ok := data["source_id"].(string); ok {
		zone.SourceID = v
		if zone.Group != "" {
			for _, r := range state.Zones {
				if r.Group == zone.Group {
					r.SourceID = v
				}
			}
		}
	}
	if v, ok := data["audio_volume"].(float64); ok {
		zone.AudioVolume = int(v)
		if zone.AudioVolume < 0 { zone.AudioVolume = 0 }
		if zone.AudioVolume > 100 { zone.AudioVolume = 100 }
	}
	if v, ok := data["audio_muted"].(bool); ok { zone.AudioMuted = v }
	if v, ok := data["mic_active"].(bool); ok { zone.MicActive = v }
	if v, ok := data["mic_volume"].(float64); ok { zone.MicVolume = int(v) }
	if v, ok := data["eq_bass"].(float64); ok { zone.EqBass = int(v) }
	if v, ok := data["eq_mid"].(float64); ok { zone.EqMid = int(v) }
	if v, ok := data["eq_treble"].(float64); ok { zone.EqTreble = int(v) }
	if v, ok := data["balance"].(float64); ok { zone.Balance = int(v) }
	if v, ok := data["audio_preset"].(string); ok { zone.AudioPreset = v }
	if v, ok := data["media_source"].(string); ok { zone.MediaSource = v }
	if v, ok := data["media_url"].(string); ok { zone.MediaURL = v }
	if v, ok := data["media_current_idx"].(float64); ok { zone.MediaCurrentIdx = int(v) }
	if v, ok := data["media_playing"].(bool); ok { zone.MediaPlaying = v }
	if v, ok := data["media_loop"].(bool); ok { zone.MediaLoop = v }
	if v, ok := data["media_shuffle"].(bool); ok { zone.MediaShuffle = v }
	if v, ok := data["screen_share_active"].(bool); ok { zone.ScreenShareActive = v }
	if v, ok := data["screen_share_mode"].(string); ok { zone.ScreenShareMode = v }
}

// ─── Main ──────────────────────────────────────────────────────

func main() {
	flag.Parse()

	if exe, err := os.Executable(); err == nil {
		appDir = filepath.Dir(exe)
	} else {
		appDir, _ = os.Getwd()
	}
	staticDir = filepath.Join(appDir, "static")
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		staticDir = "static"
	}

	state = NewAppState()
	
	// Initialize IPTV channel lineup
	initLineup()
	
	// Load settings
	loadSettings()
	
	// Initialize activity log
	initActivityLog()
	
	// Initialize dashboard
	addActivity("system", "info", "HotelMP3 server started", fmt.Sprintf("Port %d", *port))

	// Routes
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)
	mux.HandleFunc("/settings", handleSettingsPage)
	mux.HandleFunc("/dashboard", handleDashboardPage)
	mux.HandleFunc("/phone", handlePhone)
	mux.HandleFunc("/api/state", handleState)
	mux.HandleFunc("/api/zones/", handleZones)
	mux.HandleFunc("/api/all/power", handleAllPower)
	mux.HandleFunc("/api/groups/", handleGroups)
	mux.HandleFunc("/api/groups", handleGroupsList)
	mux.HandleFunc("/api/media/scan", handleMediaScan)
	mux.HandleFunc("/api/media/scan-grouped", handleMediaScanGrouped)
	mux.HandleFunc("/api/media/scan-folder", handleMediaScanFolder)
	mux.HandleFunc("/api/media/scan-usb", handleMediaScanUSB)
	mux.HandleFunc("/api/media/play", handleMediaPlay)
	mux.HandleFunc("/api/media/info", handleMediaInfo)
	mux.HandleFunc("/api/media/refresh", handleMediaRefresh)
	mux.HandleFunc("/api/media/status", handleMediaStatus)
	mux.HandleFunc("/api/youtube/stream", handleYouTubeStream)
	mux.HandleFunc("/api/youtube/direct", handleYouTubeDirect)
	mux.HandleFunc("/api/youtube/info", handleYouTubeInfo)
	mux.HandleFunc("/api/wifi/status", handleWiFiStatus)
	mux.HandleFunc("/api/wifi/scan", handleWiFiScan)
	mux.HandleFunc("/api/usb", handleUSB)
	mux.HandleFunc("/api/room-devices", handleRoomDevices)
	mux.HandleFunc("/api/qr/", handleQR)
	mux.HandleFunc("/api/screen-sources", handleScreenSources)
	mux.HandleFunc("/api/remote/connect", handleRemoteConnect)
	mux.HandleFunc("/api/remote/disconnect/", handleRemoteDisconnect)
	mux.HandleFunc("/api/remote/connections", handleRemoteConnections)
	mux.HandleFunc("/api/remote/connection-types", handleRemoteConnectionTypes)
	mux.HandleFunc("/api/audio/devices", handleAudioDevices)
	mux.HandleFunc("/api/display/devices", handleDisplayDevices)
	mux.HandleFunc("/api/cast/", handleCast)
	// IPTV Channel routes
	mux.HandleFunc("/api/channels/stream", handleChannelStream)
	mux.HandleFunc("/api/channels/tune", handleChannelTune)
	mux.HandleFunc("/api/channels/import", handleM3UImport)
	mux.HandleFunc("/api/channels/groups", handleChannelGroups)
	mux.HandleFunc("/api/channels/", handleChannelCRUD)
	// Settings routes (more specific first!)
	mux.HandleFunc("/api/settings/update", handleSettingsUpdate)
	mux.HandleFunc("/api/settings/export", handleSettingsExport)
	mux.HandleFunc("/api/settings/import", handleSettingsImport)
	mux.HandleFunc("/api/settings/reset", handleSettingsReset)
	mux.HandleFunc("/api/settings", handleSettingsGet)
	mux.HandleFunc("/api/system/info", handleSystemInfo)
	mux.HandleFunc("/api/timezones", handleTimezones)
	// Dashboard routes
	mux.HandleFunc("/api/dashboard/stats", handleDashboardStats)
	mux.HandleFunc("/api/dashboard/zones", handleDashboardZones)
	mux.HandleFunc("/api/dashboard/channels", handleDashboardChannels)
	mux.HandleFunc("/api/dashboard/activity", handleDashboardActivity)
	mux.HandleFunc("/api/dashboard/health", handleDashboardHealth)
	mux.HandleFunc("/api/dashboard/action", handleDashboardAction)
	mux.HandleFunc("/ws", handleWebSocket)

	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	ips := getAllLocalIPs()
	primaryIP := ips[0]

	fmt.Println(strings.Repeat("=", 55))
	fmt.Println("  HotelMP3 — AV Control System (Go)")
	fmt.Println("  Spatial UI + HUD Architecture")
	fmt.Printf("  Local:   http://localhost:%d\n", *port)
	fmt.Println(strings.Repeat("=", 55))
	if len(ips) > 0 {
		fmt.Println("  Open on your phone/TV/tablet:")
		for _, ip := range ips {
			fmt.Printf("  http://%s:%d\n", ip, *port)
		}
	}
	fmt.Println(strings.Repeat("=", 55))

	if !*noBrowser {
		go func() {
			time.Sleep(2 * time.Second)
			url := fmt.Sprintf("http://%s:%d", primaryIP, *port)
			switch runtime.GOOS {
			case "windows":
				exec.Command("cmd", "/c", "start", url).Start()
			case "darwin":
				exec.Command("open", url).Start()
			case "linux":
				exec.Command("xdg-open", url).Start()
			}
		}()
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("Listen failed: %v, retrying...", err)
		time.Sleep(2 * time.Second)
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			log.Fatalf("Server failed after retry: %v", err)
		}
	}

	log.Printf("Starting server on %s", addr)
	log.Printf("Static dir: %s", staticDir)
	if err := http.Serve(ln, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// ─── Stub handlers (defined in handlers.go / media.go) ─────────

func handleDashboardPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(staticDir, "dashboard.html"))
}
func handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(staticDir, "settings.html"))
}
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" { http.NotFound(w, r); return }
	http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
}
func handlePhone(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(staticDir, "phone.html"))
}
func handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" { jsonError(w, "Method not allowed", 405); return }
	jsonResponse(w, state.FullState())
}
func handleAllPower(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { jsonError(w, "Method not allowed", 405); return }
	power := r.URL.Query().Get("power") == "true"
	state.mu.Lock()
	for _, z := range state.Zones { z.Power = power }
	state.mu.Unlock()
	state.Broadcast(state.FullState())
	jsonResponse(w, map[string]bool{"power": power})
}
func handleGroupsList(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && r.Method == "GET" {
		state.mu.RLock()
		defer state.mu.RUnlock()
		jsonResponse(w, map[string]interface{}{"groups": state.GroupNames})
		return
	}
	jsonError(w, "Not found", 404)
}
func handleWiFiStatus(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{"current": map[string]interface{}{"connected": false}})
}
func handleWiFiScan(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{"networks": []map[string]interface{}{}})
}
func handleUSB(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{"devices": []map[string]interface{}{}, "count": 0})
}
func handleRoomDevices(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	result := make(map[string]map[string]interface{})
	for rid, zone := range state.Zones {
		result[rid] = map[string]interface{}{
			"display": zone.DisplayDevice, "speaker": zone.SpeakerDevice,
			"mic": zone.MicDevice, "source": zone.SourceDevice,
		}
	}
	jsonResponse(w, result)
}
func handleQR(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 { jsonError(w, "Zone ID required", 400); return }
	zoneID := parts[1]
	ip := getLocalIP()
	link := fmt.Sprintf("http://%s:%d/?zone=%s", ip, *port, zoneID)
	qrURL := fmt.Sprintf("https://api.qrserver.com/v1/create-qr-code/?size=300x300&data=%s&color=0f172a&bgcolor=f8fafc&qzone=2",
		link)
	jsonResponse(w, map[string]string{"zone_id": zoneID, "link": link, "qr_url": qrURL, "ip": ip})
}
func handleScreenSources(w http.ResponseWriter, r *http.Request) {
	sources := []map[string]interface{}{
		{"id": "screen_0", "name": "Primary Display", "type": "desktop", "resolution": "unknown", "primary": true},
		{"id": "window_capture", "name": "Application Window", "type": "window", "resolution": "variable"},
	}
	jsonResponse(w, map[string]interface{}{"sources": sources})
}
func handleRemoteConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { jsonError(w, "Method not allowed", 405); return }
	var data map[string]interface{}
	json.NewDecoder(r.Body).Decode(&data)
	connID, _ := data["conn_id"].(string)
	if connID == "" { connID = fmt.Sprintf("conn_%d", time.Now().Unix()) }
	connType, _ := data["type"].(string)
	ip, _ := data["ip"].(string)
	port := 0
	switch connType {
	case "rdp": port = 3389; case "vnc": port = 5900
	case "adb": port = 5555; case "ssh": port = 22
	}
	conn := map[string]interface{}{
		"id": connID, "type": connType, "ip": ip, "port": port,
		"device_type": data["device_type"], "room_id": data["room_id"],
		"status": "connected", "started_at": time.Now().Unix(),
	}
	state.mu.Lock()
	state.RemoteConnections[connID] = conn
	state.mu.Unlock()
	state.Broadcast(map[string]interface{}{"type": "remote_update", "connection": conn})
	jsonResponse(w, conn)
}
func handleRemoteDisconnect(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 { jsonError(w, "Connection ID required", 400); return }
	connID := parts[2]
	state.mu.Lock()
	conn, ok := state.RemoteConnections[connID]
	if ok { delete(state.RemoteConnections, connID) }
	state.mu.Unlock()
	if !ok { jsonError(w, "Connection not found", 404); return }
	state.Broadcast(map[string]interface{}{"type": "remote_update", "connection": conn, "removed": true})
	jsonResponse(w, map[string]string{"status": "disconnected"})
}
func handleRemoteConnections(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	defer state.mu.RUnlock()
	jsonResponse(w, map[string]interface{}{"connections": state.RemoteConnections})
}
func handleRemoteConnectionTypes(w http.ResponseWriter, r *http.Request) {
	types := []map[string]interface{}{
		{"id": "rdp", "name": "RDP (Windows)", "port": 3389, "icon": "🖥️"},
		{"id": "vnc", "name": "VNC", "port": 5900, "icon": "🖥️"},
		{"id": "adb", "name": "ADB (Android)", "port": 5555, "icon": "📱"},
		{"id": "ssh", "name": "SSH (Linux)", "port": 22, "icon": "🐧"},
		{"id": "airplay", "name": "AirPlay", "port": 7000, "icon": "🍎"},
		{"id": "chromecast", "name": "Chromecast", "port": 8008, "icon": "📺"},
	}
	jsonResponse(w, map[string]interface{}{"types": types})
}
func handleAudioDevices(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{
		"outputs": []map[string]interface{}{{"name": "Default Audio", "active": true}},
		"inputs": []map[string]interface{}{}, "bluetooth": []map[string]interface{}{},
	})
}
func handleDisplayDevices(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]interface{}{"monitors": []map[string]interface{}{}, "gpus": []map[string]interface{}{}})
}
func handleCast(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { jsonError(w, "Method not allowed", 405); return }
	jsonResponse(w, map[string]string{"status": "cast_sent"})
}

// ─── Zone Handler ──────────────────────────────────────────────

func handleZones(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 { jsonError(w, "Invalid path", 400); return }
	zoneID := parts[2]

	switch {
	case len(parts) == 3 && r.Method == "POST":
		var data map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			jsonError(w, err.Error(), 400); return
		}
		state.mu.Lock()
		zone, ok := state.Zones[zoneID]
		if !ok { state.mu.Unlock(); jsonError(w, "Zone not found", 404); return }
		updateZoneFromData(zone, data)
		state.mu.Unlock()
		state.Broadcast(map[string]interface{}{"type": "zone_update", "zone": zone})
		jsonResponse(w, zone)

	case len(parts) == 4 && parts[3] == "join-group" && r.Method == "POST":
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		groupID, _ := data["group_id"].(string)
		if groupID == "" { jsonError(w, "group_id required", 400); return }
		state.mu.Lock()
		zone, ok := state.Zones[zoneID]
		if !ok { state.mu.Unlock(); jsonError(w, "Zone not found", 404); return }
		zone.Group = groupID
		state.mu.Unlock()
		state.Broadcast(state.FullState())
		jsonResponse(w, zone)

	case len(parts) == 4 && parts[3] == "unjoin-group" && r.Method == "POST":
		state.mu.Lock()
		zone, ok := state.Zones[zoneID]
		if !ok { state.mu.Unlock(); jsonError(w, "Zone not found", 404); return }
		zone.Group = ""
		state.mu.Unlock()
		state.Broadcast(state.FullState())
		jsonResponse(w, zone)

	case len(parts) == 4 && parts[3] == "media" && r.Method == "POST":
		var data map[string]interface{}
		json.NewDecoder(r.Body).Decode(&data)
		action, _ := data["action"].(string)
		state.mu.Lock()
		zone, ok := state.Zones[zoneID]
		if !ok { state.mu.Unlock(); jsonError(w, "Zone not found", 404); return }
		switch action {
		case "play":
			zone.MediaPlaying = true
			if src, ok := data["source"].(string); ok { zone.MediaSource = src }
			if url, ok := data["url"].(string); ok { zone.MediaURL = url }
			if pl, ok := data["playlist"].([]interface{}); ok { zone.MediaPlaylist = pl }
			if idx, ok := data["current_idx"].(float64); ok { zone.MediaCurrentIdx = int(idx) }
		case "pause": zone.MediaPlaying = false
		case "stop": zone.MediaPlaying = false; zone.MediaPosition = 0
		case "next":
			if len(zone.MediaPlaylist) > 0 {
				zone.MediaCurrentIdx = (zone.MediaCurrentIdx + 1) % len(zone.MediaPlaylist)
			}
		case "prev":
			if len(zone.MediaPlaylist) > 0 {
				zone.MediaCurrentIdx = (zone.MediaCurrentIdx - 1 + len(zone.MediaPlaylist)) % len(zone.MediaPlaylist)
			}
		}
		state.mu.Unlock()
		state.Broadcast(map[string]interface{}{"type": "zone_update", "zone": zone})
		jsonResponse(w, map[string]string{"status": action})

	default:
		jsonError(w, "Not found", 404)
	}
}

// ─── Group Handler ─────────────────────────────────────────────

func handleGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		// Handle /api/groups/{group_id}/rename and /delete
		path := strings.Trim(r.URL.Path, "/")
		parts := strings.Split(path, "/")
		if len(parts) == 3 && r.Method == "POST" {
			groupID := parts[2]
			var data map[string]interface{}
			json.NewDecoder(r.Body).Decode(&data)
			name, _ := data["name"].(string)
			state.mu.Lock()
			if state.GroupNames == nil { state.GroupNames = make(map[string]string) }
			state.GroupNames[groupID] = name
			state.mu.Unlock()
			state.Broadcast(state.FullState())
			jsonResponse(w, map[string]string{"status": "renamed", "name": name})
			return
		}
		if len(parts) == 3 && r.Method == "DELETE" {
			groupID := parts[2]
			state.mu.Lock()
			for _, z := range state.Zones {
				if z.Group == groupID { z.Group = "" }
			}
			delete(state.GroupNames, groupID)
			state.mu.Unlock()
			state.Broadcast(state.FullState())
			jsonResponse(w, map[string]string{"status": "deleted"})
			return
		}
		jsonError(w, "Method not allowed", 405)
		return
	}
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, err.Error(), 400); return
	}
	groupID, _ := data["group_id"].(string)
	name, _ := data["name"].(string)
	state.mu.Lock()
	defer state.mu.Unlock()
	if groupID == "" {
		existing := make(map[string]bool)
		for _, r := range state.Zones {
			if r.Group != "" { existing[r.Group] = true }
		}
		for c := 'A'; c <= 'Z'; c++ {
			s := string(c)
			if !existing[s] { groupID = s; break }
		}
		if groupID == "" { groupID = fmt.Sprintf("G%d", len(state.GroupNames)+1) }
	}
	if name == "" { name = "Group " + groupID }
	if state.GroupNames == nil { state.GroupNames = make(map[string]string) }
	state.GroupNames[groupID] = name
	state.Broadcast(state.FullState())
	state.Broadcast(map[string]interface{}{"type": "groups_changed"})
	jsonResponse(w, map[string]string{"group_id": groupID, "name": name})
}

// ─── WebSocket Handler ─────────────────────────────────────────

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil { log.Printf("WS upgrade error: %v", err); return }
	defer conn.Close()

	state.AddClient(conn)
	defer state.RemoveClient(conn)

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(state.FullState()); err != nil {
		log.Printf("WS initial write error: %v", err); return
	}

	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil { break }
		msgType, _ := msg["type"].(string)
		switch msgType {
		case "update_zone":
			zoneID, _ := msg["zone_id"].(string)
			update, _ := msg["update"].(map[string]interface{})
			state.mu.Lock()
			if zone, ok := state.Zones[zoneID]; ok {
				updateZoneFromData(zone, update)
			}
			state.mu.Unlock()
			state.Broadcast(map[string]interface{}{"type": "zone_update", "zone": state.Zones[zoneID]})
		case "join_group":
			zoneID, _ := msg["zone_id"].(string)
			groupID, _ := msg["group_id"].(string)
			state.mu.Lock()
			if zone, ok := state.Zones[zoneID]; ok { zone.Group = groupID }
			state.mu.Unlock()
			state.Broadcast(state.FullState())
		case "unjoin_group":
			zoneID, _ := msg["zone_id"].(string)
			state.mu.Lock()
			if zone, ok := state.Zones[zoneID]; ok { zone.Group = "" }
			state.mu.Unlock()
			state.Broadcast(state.FullState())
		case "ping":
			conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			conn.WriteJSON(map[string]string{"type": "pong"})
		}
	}
}
