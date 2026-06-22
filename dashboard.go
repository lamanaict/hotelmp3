package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"
)

// ─── Activity Log ────────────────────────────────────────────────

type ActivityEntry struct {
	ID        string `json:"id"`
	Time      string `json:"time"`
	Timestamp int64  `json:"timestamp"`
	Category  string `json:"category"` // zone, channel, media, network, system, settings
	Type      string `json:"type"`     // info, success, warning, error, action
	Message   string `json:"message"`
	Details   string `json:"details,omitempty"`
}

type ActivityLog struct {
	mu      sync.RWMutex
	entries []ActivityEntry
	maxSize int
}

var activityLog *ActivityLog

func initActivityLog() {
	activityLog = &ActivityLog{
		entries: make([]ActivityEntry, 0, 500),
		maxSize: 500,
	}
}

func addActivity(category, msgType, message, details string) {
	activityLog.mu.Lock()
	defer activityLog.mu.Unlock()

	entry := ActivityEntry{
		ID:        fmt.Sprintf("act_%d", time.Now().UnixNano()),
		Time:      time.Now().Format("15:04:05"),
		Timestamp: time.Now().Unix(),
		Category:  category,
		Type:      msgType,
		Message:   message,
		Details:   details,
	}

	activityLog.entries = append(activityLog.entries, entry)
	if len(activityLog.entries) > activityLog.maxSize {
		activityLog.entries = activityLog.entries[len(activityLog.entries)-activityLog.maxSize:]
	}
}

func getActivities(limit int, category string) []ActivityEntry {
	activityLog.mu.RLock()
	defer activityLog.mu.RUnlock()

	if limit <= 0 {
		limit = 100
	}

	var filtered []ActivityEntry
	for i := len(activityLog.entries) - 1; i >= 0; i-- {
		if category == "" || activityLog.entries[i].Category == category {
			filtered = append(filtered, activityLog.entries[i])
			if len(filtered) >= limit {
				break
			}
		}
	}

	// Reverse to chronological order
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered
}

// ─── Dashboard Stats ─────────────────────────────────────────────

type DashboardStats struct {
	Server struct {
		Uptime       string `json:"uptime"`
		Goroutines   int    `json:"goroutines"`
		MemoryMB     string `json:"memory_mb"`
		CPUCount     int    `json:"cpu_count"`
		GoVersion    string `json:"go_version"`
		Port         int    `json:"port"`
	} `json:"server"`
	Zones struct {
		Total    int `json:"total"`
		Online   int `json:"online"`
		Offline  int `json:"offline"`
		Powered  int `json:"powered"`
		Grouped  int `json:"grouped"`
		ByType   map[string]int `json:"by_type"`
	} `json:"zones"`
	Channels struct {
		Total   int `json:"total"`
		Active  int `json:"active"`
		Offline int `json:"offline"`
		Viewers int `json:"total_viewers"`
	} `json:"channels"`
	Sources struct {
		Total int `json:"total"`
	} `json:"sources"`
	System struct {
		CPUUsage    string `json:"cpu_usage"`
		MemoryTotal string `json:"memory_total"`
		MemoryUsed  string `json:"memory_used"`
		Hostname    string `json:"hostname"`
		OS          string `json:"os"`
		PrimaryIP   string `json:"primary_ip"`
	} `json:"system"`
	Network struct {
		ConnectedClients int      `json:"connected_clients"`
		LocalIPs         []string `json:"local_ips"`
		Gateway          string   `json:"gateway"`
	} `json:"network"`
	Media struct {
		FilesFound int  `json:"files_found"`
		Scanning   bool `json:"scanning"`
	} `json:"media"`
	Settings struct {
		HotelName  string `json:"hotel_name"`
		Floors     int    `json:"floors"`
		TotalRooms int    `json:"total_rooms"`
	} `json:"settings"`
}

// ─── Dashboard API Handlers ──────────────────────────────────────

func handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}

	var stats DashboardStats

	// Server stats
	stats.Server.Uptime = formatDuration(time.Since(startTime))
	stats.Server.Goroutines = runtime.NumGoroutine()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	stats.Server.MemoryMB = fmt.Sprintf("%.1f MB", float64(m.Alloc)/(1024*1024))
	stats.Server.CPUCount = runtime.NumCPU()
	stats.Server.GoVersion = runtime.Version()
	stats.Server.Port = *port

	// Zone stats
	state.mu.RLock()
	stats.Zones.Total = len(state.Zones)
	stats.Sources.Total = len(state.Sources)
	stats.Zones.ByType = make(map[string]int)
	for _, z := range state.Zones {
		if z.Online {
			stats.Zones.Online++
		} else {
			stats.Zones.Offline++
		}
		if z.Power {
			stats.Zones.Powered++
		}
		if z.Group != "" {
			stats.Zones.Grouped++
		}
		stats.Zones.ByType[z.Type]++
	}

	// Channel stats
	stats.Channels.Total = len(lineup.GetAll())
	for _, ch := range lineup.GetAll() {
		if ch.Status == "active" {
			stats.Channels.Active++
		} else {
			stats.Channels.Offline++
		}
		stats.Channels.Viewers += ch.Viewers
	}

	// Media stats
	_, total, _, scanning := state.MediaCache.Get()
	stats.Media.FilesFound = total
	stats.Media.Scanning = scanning

	// Network
	stats.Network.ConnectedClients = len(state.Clients)
	stats.Network.LocalIPs = getAllLocalIPs()
	stats.Network.Gateway = func() string {
		g, _, _ := detectNetworkInfo()
		return g
	}()

	// System
	hostname, _ := os.Hostname()
	stats.System.Hostname = hostname
	stats.System.OS = runtime.GOOS + " (" + runtime.GOARCH + ")"
	stats.System.PrimaryIP = getLocalIP()
	stats.System.CPUUsage = getCPUUsage()
	stats.System.MemoryTotal, stats.System.MemoryUsed = getMemoryInfo()

	// Settings
	settings.mu.RLock()
	stats.Settings.HotelName = settings.HotelName
	stats.Settings.Floors = settings.Floors
	stats.Settings.TotalRooms = settings.RoomsTotal
	settings.mu.RUnlock()

	state.mu.RUnlock()

	jsonResponse(w, stats)
}

func handleDashboardZones(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	zones := make(map[string]interface{})
	for k, v := range state.Zones {
		zones[k] = v
	}
	jsonResponse(w, map[string]interface{}{
		"zones":    zones,
		"count":    len(zones),
		"channels": lineup.GetAll(),
		"sources":  state.Sources,
	})
}

func handleDashboardChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	channelMap := lineup.GetAll()
	channels := make([]interface{}, 0, len(channelMap))
	for _, ch := range channelMap {
		channels = append(channels, ch)
	}
	jsonResponse(w, map[string]interface{}{
		"channels": channels,
		"groups":   lineup.GetGroups(),
		"count":    len(channels),
	})
}

func handleDashboardActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	category := r.URL.Query().Get("category")
	limit := parseIntSafe(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	activities := getActivities(limit, category)
	jsonResponse(w, map[string]interface{}{
		"activities": activities,
		"count":      len(activities),
	})
}

func handleDashboardHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}

	// Check all critical systems
	checks := map[string]bool{
		"server":     true,
		"websocket":  true,
		"channels":   len(lineup.GetAll()) > 0,
		"zones":      len(state.Zones) > 0,
		"settings":   settings != nil,
		"lineup":     true,
	}

	// Try a simple PowerShell command to verify system access
	psResult := runPS("Get-Date -Format 'HH:mm'")
	checks["system_access"] = psResult != ""

	allHealthy := true
	for _, v := range checks {
		if !v {
			allHealthy = false
			break
		}
	}

	status := "healthy"
	if !allHealthy {
		status = "degraded"
	}

	jsonResponse(w, map[string]interface{}{
		"status":    status,
		"checks":    checks,
		"timestamp": time.Now().Unix(),
	})
}

func handleDashboardAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "Method not allowed", 405)
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}

	action, _ := data["action"].(string)
	target, _ := data["target"].(string)

	switch action {
	case "reboot_zone":
		if target == "" {
			jsonError(w, "target zone_id required", 400)
			return
		}
		state.mu.Lock()
		if zone, ok := state.Zones[target]; ok {
			zone.Power = false
			zone.Online = false
		}
		state.mu.Unlock()
		addActivity("zone", "action", "Zone "+target+" reboot initiated", "")
		state.Broadcast(map[string]interface{}{"type": "zone_update", "zone": state.Zones[target]})
		jsonResponse(w, map[string]string{"status": "ok", "action": "reboot_zone"})

	case "power_all_on":
		state.mu.Lock()
		for _, z := range state.Zones {
			z.Power = true
			z.Online = true
		}
		state.mu.Unlock()
		addActivity("zone", "action", "All zones powered ON", "")
		state.Broadcast(state.FullState())
		jsonResponse(w, map[string]string{"status": "ok", "action": "power_all_on"})

	case "power_all_off":
		state.mu.Lock()
		for _, z := range state.Zones {
			z.Power = false
		}
		state.mu.Unlock()
		addActivity("zone", "action", "All zones powered OFF", "")
		state.Broadcast(state.FullState())
		jsonResponse(w, map[string]string{"status": "ok", "action": "power_all_off"})

	case "scan_media":
		go RunFullScan()
		addActivity("media", "action", "Media scan started", "")
		jsonResponse(w, map[string]string{"status": "ok", "action": "scan_media"})

	case "refresh_channels":
		addActivity("channel", "action", "Channel lineup refreshed", "")
		state.Broadcast(map[string]interface{}{"type": "lineup_refresh"})
		jsonResponse(w, map[string]string{"status": "ok", "action": "refresh_channels"})

	case "clear_activity":
		activityLog.mu.Lock()
		activityLog.entries = make([]ActivityEntry, 0, 500)
		activityLog.mu.Unlock()
		addActivity("system", "action", "Activity log cleared", "")
		jsonResponse(w, map[string]string{"status": "ok", "action": "clear_activity"})

	default:
		jsonError(w, "Unknown action: "+action, 400)
	}
}
