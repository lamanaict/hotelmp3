package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── Settings Types ──────────────────────────────────────────────

type Settings struct {
	mu sync.RWMutex

	// Location
	HotelName    string `json:"hotel_name"`
	Address      string `json:"address"`
	Timezone     string `json:"timezone"`
	Floors       int    `json:"floors"`
	Wings        string `json:"wings"`
	RoomsTotal   int    `json:"rooms_total"`

	// Time & Date
	TimeFormat   string `json:"time_format"`   // "12h" or "24h"
	DSTEnabled   bool   `json:"dst_enabled"`
	NTPServer    string `json:"ntp_server"`
	DateTime     string `json:"date_time"`     // manual override, empty = use system

	// Network
	ServerPort   int    `json:"server_port"`
	AllowRemote  bool   `json:"allow_remote"`
	APINetwork   string `json:"api_network"`   // "lan", "wan", "localhost"

	// Audio I/O
	DefaultAudioOutput  string `json:"default_audio_output"`
	DefaultAudioInput   string `json:"default_audio_input"`
	AudioSampleRate     int    `json:"audio_sample_rate"`
	AudioBitDepth       int    `json:"audio_bit_depth"`
	AudioChannels       int    `json:"audio_channels"`

	// Video I/O
	DefaultVideoOutput  string `json:"default_video_output"`
	DefaultVideoInput   string `json:"default_video_input"`
	VideoResolution     string `json:"video_resolution"`
	VideoRefreshRate    int    `json:"video_refresh_rate"`

	// Screen / Display
	ScreenWidth     int    `json:"screen_width"`
	ScreenHeight    int    `json:"screen_height"`
	AspectRatio     string `json:"aspect_ratio"`
	OverscanPercent int    `json:"overscan_percent"`
	DisplayCount    int    `json:"display_count"`
}

type SystemInfo struct {
	Hostname       string                   `json:"hostname"`
	OS             string                   `json:"os"`
	Arch           string                   `json:"arch"`
	GoVersion      string                   `json:"go_version"`
	Uptime         string                   `json:"uptime"`
	LocalIPs       []string                 `json:"local_ip"`
	PrimaryIP      string                   `json:"primary_ip"`
	MACAddresses   []string                 `json:"mac_addresses"`
	Gateway        string                   `json:"gateway"`
	DNSServers     []string                 `json:"dns_servers"`
	AudioOutputs   []map[string]interface{} `json:"audio_outputs"`
	AudioInputs    []map[string]interface{} `json:"audio_inputs"`
	Displays       []map[string]interface{} `json:"displays"`
	VideoInputs    []map[string]interface{} `json:"video_inputs"`
	CPUUsage       string                   `json:"cpu_usage"`
	MemoryTotal    string                   `json:"memory_total"`
	MemoryUsed     string                   `json:"memory_used"`
	ServerPort     int                      `json:"server_port"`
	ConnectedWS    int                      `json:"connected_ws_clients"`
}

var settings *Settings
var startTime = time.Now()

func defaultSettings() *Settings {
	return &Settings{
		HotelName:          "My Hotel",
		Address:            "",
		Timezone:           "Local",
		Floors:             10,
		Wings:              "A,B,C,D,E,F,G,H",
		RoomsTotal:         500,
		TimeFormat:         "24h",
		DSTEnabled:         true,
		NTPServer:          "pool.ntp.org",
		ServerPort:         8000,
		AllowRemote:        true,
		APINetwork:         "lan",
		DefaultAudioOutput: "Default",
		DefaultAudioInput:  "Default",
		AudioSampleRate:    48000,
		AudioBitDepth:      16,
		AudioChannels:      2,
		DefaultVideoOutput: "Primary",
		DefaultVideoInput:  "None",
		VideoResolution:    "auto",
		VideoRefreshRate:   60,
		ScreenWidth:        1920,
		ScreenHeight:       1080,
		AspectRatio:        "16:9",
		OverscanPercent:    0,
		DisplayCount:       1,
	}
}

func settingsPath() string {
	return filepath.Join(appDir, "settings.json")
}

func loadSettings() {
	settings = defaultSettings()
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		log.Printf("No settings.json found, using defaults")
		saveSettings()
		return
	}
	if err := json.Unmarshal(data, settings); err != nil {
		log.Printf("Settings parse error: %v, using defaults", err)
		settings = defaultSettings()
		return
	}
	log.Printf("Loaded settings for: %s", settings.HotelName)
}

func saveSettings() {
	settings.mu.RLock()
	defer settings.mu.RUnlock()
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		log.Printf("Settings marshal error: %v", err)
		return
	}
	if err := os.WriteFile(settingsPath(), data, 0644); err != nil {
		log.Printf("Settings write error: %v", err)
	}
}

func updateSettings(updates map[string]interface{}) {
	settings.mu.Lock()
	defer settings.mu.Unlock()

	if v, ok := updates["hotel_name"].(string); ok { settings.HotelName = v }
	if v, ok := updates["address"].(string); ok { settings.Address = v }
	if v, ok := updates["timezone"].(string); ok { settings.Timezone = v }
	if v, ok := updates["floors"].(float64); ok { settings.Floors = int(v) }
	if v, ok := updates["wings"].(string); ok { settings.Wings = v }
	if v, ok := updates["rooms_total"].(float64); ok { settings.RoomsTotal = int(v) }
	if v, ok := updates["time_format"].(string); ok { settings.TimeFormat = v }
	if v, ok := updates["dst_enabled"].(bool); ok { settings.DSTEnabled = v }
	if v, ok := updates["ntp_server"].(string); ok { settings.NTPServer = v }
	if v, ok := updates["date_time"].(string); ok { settings.DateTime = v }
	if v, ok := updates["server_port"].(float64); ok { settings.ServerPort = int(v) }
	if v, ok := updates["allow_remote"].(bool); ok { settings.AllowRemote = v }
	if v, ok := updates["api_network"].(string); ok { settings.APINetwork = v }
	if v, ok := updates["default_audio_output"].(string); ok { settings.DefaultAudioOutput = v }
	if v, ok := updates["default_audio_input"].(string); ok { settings.DefaultAudioInput = v }
	if v, ok := updates["audio_sample_rate"].(float64); ok { settings.AudioSampleRate = int(v) }
	if v, ok := updates["audio_bit_depth"].(float64); ok { settings.AudioBitDepth = int(v) }
	if v, ok := updates["audio_channels"].(float64); ok { settings.AudioChannels = int(v) }
	if v, ok := updates["default_video_output"].(string); ok { settings.DefaultVideoOutput = v }
	if v, ok := updates["default_video_input"].(string); ok { settings.DefaultVideoInput = v }
	if v, ok := updates["video_resolution"].(string); ok { settings.VideoResolution = v }
	if v, ok := updates["video_refresh_rate"].(float64); ok { settings.VideoRefreshRate = int(v) }
	if v, ok := updates["screen_width"].(float64); ok { settings.ScreenWidth = int(v) }
	if v, ok := updates["screen_height"].(float64); ok { settings.ScreenHeight = int(v) }
	if v, ok := updates["aspect_ratio"].(string); ok { settings.AspectRatio = v }
	if v, ok := updates["overscan_percent"].(float64); ok { settings.OverscanPercent = int(v) }
	if v, ok := updates["display_count"].(float64); ok { settings.DisplayCount = int(v) }

	saveSettings()
}

// ─── System Detection (Windows via PowerShell) ───────────────────

func runPS(cmd string) string {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", cmd).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func detectAudioDevices() ([]map[string]interface{}, []map[string]interface{}) {
	var outputs []map[string]interface{}
	var inputs []map[string]interface{}

	// Get audio output devices (speakers, HDMI audio, etc.)
	out := runPS("Get-AudioDevice -List | Where-Object { $_.Type -eq 'Playback' } | ForEach-Object { Write-Output \"$($_.Name)|$($_.ID)|$($._Default)\" }")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 2 {
				def := len(parts) > 2 && strings.ToLower(parts[2]) == "true"
				outputs = append(outputs, map[string]interface{}{
					"name":    strings.TrimSpace(parts[0]),
					"id":      strings.TrimSpace(parts[1]),
					"default": def,
				})
			}
		}
	}

	// Get audio input devices (microphones, line-in)
	out = runPS("Get-AudioDevice -List | Where-Object { $_.Type -eq 'Recording' } | ForEach-Object { Write-Output \"$($_.Name)|$($_.ID)|$($._Default)\" }")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 2 {
				def := len(parts) > 2 && strings.ToLower(parts[2]) == "true"
				inputs = append(inputs, map[string]interface{}{
					"name":    strings.TrimSpace(parts[0]),
					"id":      strings.TrimSpace(parts[1]),
					"default": def,
				})
			}
		}
	}

	// Fallback: use PowerShell to get audio devices via WMI
	if len(outputs) == 0 {
		out = runPS("Get-WmiObject Win32_SoundDevice | ForEach-Object { Write-Output \"$($_.Name)|$($_.Status)\" }")
		if out != "" {
			for _, line := range strings.Split(out, "\n") {
				parts := strings.SplitN(line, "|", 2)
				if len(parts) >= 1 {
					name := strings.TrimSpace(parts[0])
					if name != "" {
						outputs = append(outputs, map[string]interface{}{
							"name":   name,
							"status": safeGet(parts, 1),
						})
					}
				}
			}
		}
	}

	return outputs, inputs
}

func detectDisplays() []map[string]interface{} {
	var displays []map[string]interface{}

	// Get monitor info via WMI
	out := runPS("Get-WmiObject Win32_VideoController | ForEach-Object { Write-Output \"$($_.Name)|$($_.CurrentHorizontalResolution)|$($_.CurrentVerticalResolution)|$($_.CurrentRefreshRate)|$($_.AdapterRAM)\" }")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 5)
			if len(parts) >= 3 {
				w := parseIntSafe(parts[1])
				h := parseIntSafe(parts[2])
				refresh := parseIntSafe(parts[3])
				vram := parts[4]
				displays = append(displays, map[string]interface{}{
					"name":      strings.TrimSpace(parts[0]),
					"width":     w,
					"height":    h,
					"refresh":   refresh,
					"vram":      vram,
					"primary":   len(displays) == 0,
				})
			}
		}
	}

	// Get monitor physical info
	out = runPS("Get-WmiObject WIMonitorID -Namespace root\\wmi | ForEach-Object { Write-Output \"$($_.InstanceName)|$($_.UserFriendlyName)|$($_.MaxHorizontalImageSize)|$($_.MaxVerticalImageSize)\" }")
	if out != "" {
		i := 0
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) >= 2 && i < len(displays) {
				name := strings.TrimSpace(parts[1])
				if name == "" {
					name = strings.TrimSpace(parts[0])
				}
				cmH := parseIntSafe(safeGet(parts, 2))
				cmV := parseIntSafe(safeGet(parts, 3))
				// Convert cm to inches (diagonal)
				diagInch := 0
				if cmH > 0 && cmV > 0 {
					diagCm := mathSqrt(float64(cmH*cmH + cmV*cmV))
					diagInch = int(diagCm / 2.54)
				}
				displays[i]["model"] = name
				displays[i]["size_inches"] = diagInch
				displays[i]["phys_width_cm"] = cmH
				displays[i]["phys_height_cm"] = cmV
				i++
			}
		}
	}

	return displays
}

func detectVideoInputs() []map[string]interface{} {
	var inputs []map[string]interface{}

	// Check for capture devices
	out := runPS("Get-WmiObject Win32_PnPEntity | Where-Object { $_.Name -match 'capture|video input|hdmi input|sd|webcam|camera' } | ForEach-Object { Write-Output \"$($_.Name)|$($_.Status)\" }")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 2)
			if len(parts) >= 1 {
				name := strings.TrimSpace(parts[0])
				if name != "" {
					inputs = append(inputs, map[string]interface{}{
						"name":   name,
						"status": safeGet(parts, 1),
						"type":   "capture",
					})
				}
			}
		}
	}

	// Check for connected displays as video inputs (HDMI-in on capture cards)
	out = runPS("Get-WmiObject Win32_VideoController | Where-Object { $_.Name -match 'capture|blackmagic|magewell|avermedia|elgato' } | ForEach-Object { Write-Output \"$($_.Name)\" }")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			name := strings.TrimSpace(line)
			if name != "" {
				inputs = append(inputs, map[string]interface{}{
					"name": name,
					"type": "hdmi_capture",
				})
			}
		}
	}

	return inputs
}

func detectNetworkInfo() (gateway string, dns []string, macs []string) {
	// Default gateway
	out := runPS("Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Select-Object -First 1 | ForEach-Object { Write-Output $_.NextHop }")
	gateway = strings.TrimSpace(out)

	// DNS servers
	out = runPS("Get-DnsClientServerAddress -AddressFamily IPv4 | Where-Object { $_.ServerAddresses } | ForEach-Object { $_.ServerAddresses -join ',' }")
	if out != "" {
		seen := map[string]bool{}
		for _, line := range strings.Split(out, "\n") {
			for _, s := range strings.Split(line, ",") {
				d := strings.TrimSpace(s)
				if d != "" && !seen[d] {
					seen[d] = true
					dns = append(dns, d)
				}
			}
		}
	}

	// MAC addresses
	out = runPS("Get-NetAdapter -Physical | Where-Object { $_.Status -eq 'Up' } | ForEach-Object { Write-Output \"$($_.MacAddress)|$($_.Name)|$($_.LinkSpeed)\" }")
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 3)
			if len(parts) >= 1 {
				mac := strings.TrimSpace(parts[0])
				if mac != "" {
					macs = append(macs, mac)
				}
			}
		}
	}

	return
}

func getMemoryInfo() (total, used string) {
	out := runPS("Get-WmiObject Win32_OperatingSystem | ForEach-Object { Write-Output \"$($_.TotalVisibleMemorySize)|$($_.FreePhysicalMemory)\" }")
	if out != "" {
		parts := strings.SplitN(out, "|", 2)
		if len(parts) == 2 {
			totalKB := parseIntSafe(parts[0])
			freeKB := parseIntSafe(parts[1])
			usedKB := totalKB - freeKB
			total = formatSize(int64(totalKB) * 1024)
			used = fmt.Sprintf("%s / %s", formatSize(int64(usedKB)*1024), total)
			return
		}
	}
	return "Unknown", "Unknown"
}

func getCPUUsage() string {
	out := runPS("Get-WmiObject Win32_Processor | Measure-Object -Property LoadPercentage -Average | ForEach-Object { Write-Output $_.Average }")
	if out != "" {
		return strings.TrimSpace(out) + "%"
	}
	return "Unknown"
}

// ─── Helpers ─────────────────────────────────────────────────────

func safeGet(arr []string, i int) string {
	if i < len(arr) {
		return strings.TrimSpace(arr[i])
	}
	return ""
}

func parseIntSafe(s string) int {
	s = strings.TrimSpace(s)
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}

func mathSqrt(x float64) float64 {
	// Simple Newton's method
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 20; i++ {
		z = (z + x/z) / 2
	}
	return z
}

// ─── Settings API Handlers ───────────────────────────────────────

func handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	settings.mu.RLock()
	defer settings.mu.RUnlock()
	jsonResponse(w, settings)
}

func handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "PUT" && r.Method != "POST" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		jsonError(w, err.Error(), 400)
		return
	}
	updateSettings(updates)
	state.Broadcast(map[string]interface{}{"type": "settings_update"})
	jsonResponse(w, map[string]string{"status": "saved"})
}

func handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}

	hostname, _ := os.Hostname()
	gateway, dnsServers, macAddresses := detectNetworkInfo()
	audioOut, audioIn := detectAudioDevices()
	displays := detectDisplays()
	videoInputs := detectVideoInputs()
	memTotal, memUsed := getMemoryInfo()

	// Count WebSocket clients
	state.mu.RLock()
	wsCount := len(state.Clients)
	state.mu.RUnlock()

	info := SystemInfo{
		Hostname:     hostname,
		OS:           runtime.GOOS + " (" + runtime.GOARCH + ")",
		Arch:         runtime.GOARCH,
		GoVersion:    runtime.Version(),
		Uptime:       formatDuration(time.Since(startTime)),
		LocalIPs:     getAllLocalIPs(),
		PrimaryIP:    getLocalIP(),
		MACAddresses: macAddresses,
		Gateway:      gateway,
		DNSServers:   dnsServers,
		AudioOutputs: audioOut,
		AudioInputs:  audioIn,
		Displays:     displays,
		VideoInputs:  videoInputs,
		CPUUsage:     getCPUUsage(),
		MemoryTotal:  memTotal,
		MemoryUsed:   memUsed,
		ServerPort:   *port,
		ConnectedWS:  wsCount,
	}

	jsonResponse(w, info)
}

func handleSettingsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	settings.mu.RLock()
	defer settings.mu.RUnlock()
	data, _ := json.MarshalIndent(settings, "", "  ")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=settings.json")
	w.Write(data)
}

func handleSettingsImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	var imported Settings
	if err := json.NewDecoder(r.Body).Decode(&imported); err != nil {
		jsonError(w, "Invalid JSON: "+err.Error(), 400)
		return
	}
	// Apply all fields
	updateSettings(map[string]interface{}{
		"hotel_name":           imported.HotelName,
		"address":              imported.Address,
		"timezone":             imported.Timezone,
		"floors":               imported.Floors,
		"wings":                imported.Wings,
		"rooms_total":          imported.RoomsTotal,
		"time_format":          imported.TimeFormat,
		"dst_enabled":          imported.DSTEnabled,
		"ntp_server":           imported.NTPServer,
		"server_port":          imported.ServerPort,
		"allow_remote":         imported.AllowRemote,
		"api_network":          imported.APINetwork,
		"default_audio_output": imported.DefaultAudioOutput,
		"default_audio_input":  imported.DefaultAudioInput,
		"audio_sample_rate":    imported.AudioSampleRate,
		"audio_bit_depth":      imported.AudioBitDepth,
		"audio_channels":       imported.AudioChannels,
		"default_video_output": imported.DefaultVideoOutput,
		"default_video_input":  imported.DefaultVideoInput,
		"video_resolution":     imported.VideoResolution,
		"video_refresh_rate":   imported.VideoRefreshRate,
		"screen_width":         imported.ScreenWidth,
		"screen_height":        imported.ScreenHeight,
		"aspect_ratio":         imported.AspectRatio,
		"overscan_percent":     imported.OverscanPercent,
		"display_count":        imported.DisplayCount,
	})
	jsonResponse(w, map[string]string{"status": "imported"})
}

func handleSettingsReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	settings = defaultSettings()
	saveSettings()
	state.Broadcast(map[string]interface{}{"type": "settings_update"})
	jsonResponse(w, map[string]string{"status": "reset"})
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// ─── Timezone List ───────────────────────────────────────────────

func handleTimezones(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	// Common timezones for hotels
	zones := []string{
		"Local",
		"UTC",
		"Australia/Sydney", "Australia/Melbourne", "Australia/Brisbane",
		"Australia/Perth", "Australia/Adelaide", "Australia/Darwin",
		"Pacific/Auckland", "Pacific/Fiji", "Pacific/Guam",
		"Asia/Tokyo", "Asia/Shanghai", "Asia/Hong_Kong", "Asia/Singapore",
		"Asia/Bangkok", "Asia/Dubai", "Asia/Kolkata", "Asia/Seoul",
		"Europe/London", "Europe/Paris", "Europe/Berlin", "Europe/Madrid",
		"Europe/Rome", "Europe/Amsterdam", "Europe/Zurich",
		"America/New_York", "America/Chicago", "America/Denver",
		"America/Los_Angeles", "America/Toronto", "America/Vancouver",
		"America/Mexico_City", "America/Sao_Paulo", "America/Buenos_Aires",
		"Africa/Cairo", "Africa/Johannesburg", "Africa/Lagos",
	}
	sort.Strings(zones)
	jsonResponse(w, map[string]interface{}{"timezones": zones})
}
