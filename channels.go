package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ─── IPTV Channel Types ──────────────────────────────────────────

type IPTVChannel struct {
	ID          string `json:"id"`
	Number      int    `json:"number"`
	Name        string `json:"name"`
	Logo        string `json:"logo"`
	Group       string `json:"group"`
	StreamURL   string `json:"stream_url"`
	Category    string `json:"category"`
	Status      string `json:"status"` // active, offline, testing
	Viewers     int    `json:"viewers"`
	EPGID       string `json:"epg_id"`
}

type ChannelLineup struct {
	mu       sync.RWMutex
	Channels map[string]*IPTVChannel `json:"channels"`
	Groups   map[string]string       `json:"groups"` // group name -> display name
}

var lineup *ChannelLineup

func initLineup() {
	lineup = &ChannelLineup{
		Channels: make(map[string]*IPTVChannel),
		Groups:   make(map[string]string),
	}
	// Load saved lineup if exists
	loadLineup()
}

func (cl *ChannelLineup) GetAll() map[string]*IPTVChannel {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	result := make(map[string]*IPTVChannel, len(cl.Channels))
	for k, v := range cl.Channels {
		result[k] = v
	}
	return result
}

func (cl *ChannelLineup) Get(id string) (*IPTVChannel, bool) {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	ch, ok := cl.Channels[id]
	return ch, ok
}

func (cl *ChannelLineup) Add(ch *IPTVChannel) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.Channels[ch.ID] = ch
	saveLineup()
}

func (cl *ChannelLineup) Update(id string, updates map[string]interface{}) (*IPTVChannel, bool) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	ch, ok := cl.Channels[id]
	if !ok {
		return nil, false
	}
	if v, ok := updates["name"].(string); ok { ch.Name = v }
	if v, ok := updates["number"].(float64); ok { ch.Number = int(v) }
	if v, ok := updates["logo"].(string); ok { ch.Logo = v }
	if v, ok := updates["group"].(string); ok { ch.Group = v }
	if v, ok := updates["stream_url"].(string); ok { ch.StreamURL = v }
	if v, ok := updates["category"].(string); ok { ch.Category = v }
	if v, ok := updates["status"].(string); ok { ch.Status = v }
	if v, ok := updates["epg_id"].(string); ok { ch.EPGID = v }
	saveLineup()
	return ch, true
}

func (cl *ChannelLineup) Delete(id string) bool {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if _, ok := cl.Channels[id]; !ok {
		return false
	}
	delete(cl.Channels, id)
	saveLineup()
	return true
}

func (cl *ChannelLineup) GetGroups() map[string]string {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	result := make(map[string]string, len(cl.Groups))
	for k, v := range cl.Groups {
		result[k] = v
	}
	return result
}

func (cl *ChannelLineup) SetGroup(name, display string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.Groups[name] = display
	saveLineup()
}

func (cl *ChannelLineup) IncrementViewers(id string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if ch, ok := cl.Channels[id]; ok {
		ch.Viewers++
	}
}

func (cl *ChannelLineup) DecrementViewers(id string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if ch, ok := cl.Channels[id]; ok && ch.Viewers > 0 {
		ch.Viewers--
	}
}

// ─── Persistence ─────────────────────────────────────────────────

func lineupPath() string {
	return filepath.Join(appDir, "lineup.json")
}

func saveLineup() {
	data, err := json.MarshalIndent(lineup.Channels, "", "  ")
	if err != nil {
		log.Printf("Lineup save error: %v", err)
		return
	}
	if err := os.WriteFile(lineupPath(), data, 0644); err != nil {
		log.Printf("Lineup write error: %v", err)
	}
}

func loadLineup() {
	data, err := os.ReadFile(lineupPath())
	if err != nil {
		// No saved lineup — seed with defaults
		seedDefaultChannels()
		return
	}
	var channels map[string]*IPTVChannel
	if err := json.Unmarshal(data, &channels); err != nil {
		log.Printf("Lineup load error: %v", err)
		seedDefaultChannels()
		return
	}
	lineup.Channels = channels
	log.Printf("Loaded %d channels from lineup.json", len(channels))
}

func seedDefaultChannels() {
	defaults := []*IPTVChannel{
		{ID: "ch1", Number: 1, Name: "BBC World News", Logo: "https://upload.wikimedia.org/wikipedia/commons/thumb/4/41/BBC_Logo_2021.svg/200px-BBC_Logo_2021.svg.png", Group: "News", StreamURL: "", Category: "News", Status: "active", EPGID: "bbc.world"},
		{ID: "ch2", Number: 2, Name: "CNN International", Logo: "", Group: "News", StreamURL: "", Category: "News", Status: "active", EPGID: "cnn.intl"},
		{ID: "ch3", Number: 3, Name: "ESPN", Logo: "", Group: "Sports", StreamURL: "", Category: "Sports", Status: "active", EPGID: "espn"},
		{ID: "ch4", Number: 4, Name: "Discovery Channel", Logo: "", Group: "Documentary", StreamURL: "", Category: "Documentary", Status: "active", EPGID: "discovery"},
		{ID: "ch5", Number: 5, Name: "HBO", Logo: "", Group: "Movies", StreamURL: "", Category: "Movies", Status: "active", EPGID: "hbo"},
		{ID: "ch6", Number: 6, Name: "National Geographic", Logo: "", Group: "Documentary", StreamURL: "", Category: "Documentary", Status: "active", EPGID: "natgeo"},
		{ID: "ch7", Number: 7, Name: "MTV", Logo: "", Group: "Music", StreamURL: "", Category: "Music", Status: "active", EPGID: "mtv"},
		{ID: "ch8", Number: 8, Name: "Cartoon Network", Logo: "", Group: "Kids", StreamURL: "", Category: "Kids", Status: "active", EPGID: "cartoon.network"},
		{ID: "ch9", Number: 9, Name: "Al Jazeera English", Logo: "", Group: "News", StreamURL: "", Category: "News", Status: "active", EPGID: "aljazeera"},
		{ID: "ch10", Number: 10, Name: "Food Network", Logo: "", Group: "Lifestyle", StreamURL: "", Category: "Lifestyle", Status: "active", EPGID: "food.network"},
		{ID: "ch11", Number: 11, Name: "History Channel", Logo: "", Group: "Documentary", StreamURL: "", Category: "Documentary", Status: "active", EPGID: "history"},
		{ID: "ch12", Number: 12, Name: "Comedy Central", Logo: "", Group: "Entertainment", StreamURL: "", Category: "Entertainment", Status: "active", EPGID: "comedy.central"},
		{ID: "ch13", Number: 13, Name: "Sky Sports", Logo: "", Group: "Sports", StreamURL: "", Category: "Sports", Status: "active", EPGID: "sky.sports"},
		{ID: "ch14", Number: 14, Name: "Fox News", Logo: "", Group: "News", StreamURL: "", Category: "News", Status: "active", EPGID: "fox.news"},
		{ID: "ch15", Number: 15, Name: "TLC", Logo: "", Group: "Lifestyle", StreamURL: "", Category: "Lifestyle", Status: "active", EPGID: "tlc"},
		{ID: "ch16", Number: 16, Name: "Nickelodeon", Logo: "", Group: "Kids", StreamURL: "", Category: "Kids", Status: "active", EPGID: "nick"},
	}
	for _, ch := range defaults {
		lineup.Channels[ch.ID] = ch
	}
	// Seed group display names
	lineup.Groups = map[string]string{
		"News":          "News & Current Affairs",
		"Sports":        "Sports",
		"Documentary":   "Documentary & Education",
		"Movies":        "Movies & Series",
		"Music":         "Music & Entertainment",
		"Kids":          "Kids & Family",
		"Lifestyle":     "Lifestyle & Food",
		"Entertainment": "Entertainment & Comedy",
	}
	saveLineup()
	log.Printf("Seeded %d default channels", len(defaults))
}

// ─── M3U Parser ──────────────────────────────────────────────────

func parseM3U(data string) []*IPTVChannel {
	var channels []*IPTVChannel
	var current *IPTVChannel
	number := 0

	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || line == "#EXTM3U" {
			continue
		}

		if strings.HasPrefix(line, "#EXTINF:") {
			number++
			current = &IPTVChannel{
				ID:     fmt.Sprintf("m3u_%d", number),
				Number: number,
				Status: "active",
			}

			// Extract attributes
			if m := regexp.MustCompile(`tvg-id="([^"]*)"`).FindStringSubmatch(line); len(m) > 1 {
				current.EPGID = m[1]
			}
			if m := regexp.MustCompile(`tvg-name="([^"]*)"`).FindStringSubmatch(line); len(m) > 1 {
				current.Name = m[1]
			}
			if m := regexp.MustCompile(`tvg-logo="([^"]*)"`).FindStringSubmatch(line); len(m) > 1 {
				current.Logo = m[1]
			}
			if m := regexp.MustCompile(`group-title="([^"]*)"`).FindStringSubmatch(line); len(m) > 1 {
				current.Group = m[1]
			}

			// Extract display name after last comma
			if idx := strings.LastIndex(line, ","); idx != -1 {
				displayName := strings.TrimSpace(line[idx+1:])
				if displayName != "" && current.Name == "" {
					current.Name = displayName
				}
			}

			if current.Name == "" {
				current.Name = fmt.Sprintf("Channel %d", number)
			}
		} else if !strings.HasPrefix(line, "#") && current != nil {
			current.StreamURL = line
			current.Category = current.Group
			channels = append(channels, current)
			current = nil
		}
	}

	return channels
}

// ─── Channel API Handlers ────────────────────────────────────────

func handleChannelLineup(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	channels := lineup.GetAll()
	jsonResponse(w, map[string]interface{}{
		"channels": channels,
		"groups":   lineup.GetGroups(),
		"count":    len(channels),
	})
}

func handleChannelCRUD(w http.ResponseWriter, r *http.Request) {
	// Path: /api/channels/ or /api/channels/{id}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	// /api/channels - list or create
	if len(parts) == 2 {
		switch r.Method {
		case "GET":
			handleChannelLineup(w, r)
		case "POST":
			var ch IPTVChannel
			if err := json.NewDecoder(r.Body).Decode(&ch); err != nil {
				jsonError(w, err.Error(), 400)
				return
			}
			if ch.ID == "" {
				ch.ID = fmt.Sprintf("ch_%d", time.Now().UnixNano())
			}
			if ch.Status == "" {
				ch.Status = "active"
			}
			lineup.Add(&ch)
			state.Broadcast(map[string]interface{}{"type": "channel_update", "channel": &ch})
			jsonResponse(w, &ch)
		default:
			jsonError(w, "Method not allowed", 405)
		}
		return
	}

	// /api/channels/{id} - get, update, delete
	if len(parts) == 3 {
		chID := parts[2]
		switch r.Method {
		case "GET":
			ch, ok := lineup.Get(chID)
			if !ok {
				jsonError(w, "Channel not found", 404)
				return
			}
			jsonResponse(w, ch)
		case "PUT", "POST":
			var updates map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
				jsonError(w, err.Error(), 400)
				return
			}
			ch, ok := lineup.Update(chID, updates)
			if !ok {
				jsonError(w, "Channel not found", 404)
				return
			}
			state.Broadcast(map[string]interface{}{"type": "channel_update", "channel": ch})
			jsonResponse(w, ch)
		case "DELETE":
			if !lineup.Delete(chID) {
				jsonError(w, "Channel not found", 404)
				return
			}
			state.Broadcast(map[string]interface{}{"type": "channel_delete", "channel_id": chID})
			jsonResponse(w, map[string]string{"status": "deleted"})
		default:
			jsonError(w, "Method not allowed", 405)
		}
		return
	}

	jsonError(w, "Not found", 404)
}

func handleChannelStream(w http.ResponseWriter, r *http.Request) {
	chID := r.URL.Query().Get("id")
	if chID == "" {
		jsonError(w, "Channel ID required", 400)
		return
	}

	ch, ok := lineup.Get(chID)
	if !ok {
		jsonError(w, "Channel not found", 404)
		return
	}

	if ch.StreamURL == "" {
		jsonError(w, "No stream URL configured for this channel", 404)
		return
	}

	// Increment viewer count
	lineup.IncrementViewers(chID)
	defer lineup.DecrementViewers(chID)

	// Parse the stream URL
	streamURL, err := url.Parse(ch.StreamURL)
	if err != nil {
		jsonError(w, "Invalid stream URL", 500)
		return
	}

	// Create upstream request
	req, err := http.NewRequest(r.Method, ch.StreamURL, nil)
	if err != nil {
		jsonError(w, "Failed to create upstream request", 500)
		return
	}

	// Forward relevant headers
	if ua := r.Header.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	if rh := r.Header.Get("Range"); rh != "" {
		req.Header.Set("Range", rh)
	}
	// Set referer if needed for IPTV sources
	if streamURL.Host != "" {
		req.Header.Set("Referer", fmt.Sprintf("%s://%s/", streamURL.Scheme, streamURL.Host))
	}

	// Make the upstream request
	client := &http.Client{
		Timeout: 0, // No timeout for streaming
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil // Follow all redirects
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Stream proxy error for channel %s (%s): %v", ch.Name, chID, err)
		jsonError(w, "Stream unavailable: "+err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	// Forward response headers
	for k, v := range resp.Header {
		// Skip hop-by-hop headers
		if k == "Transfer-Encoding" || k == "Connection" || k == "Alt-Svc" || k == "Keep-Alive" {
			continue
		}
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	// Ensure CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	// Set content type if not already set
	if w.Header().Get("Content-Type") == "" {
		ct := resp.Header.Get("Content-Type")
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		} else {
			// Default based on common IPTV formats
			if strings.Contains(ch.StreamURL, ".m3u8") {
				w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			} else if strings.Contains(ch.StreamURL, ".ts") {
				w.Header().Set("Content-Type", "video/mp2t")
			} else {
				w.Header().Set("Content-Type", "video/mp2t")
			}
		}
	}

	// Write status and stream body
	w.WriteHeader(resp.StatusCode)

	buf := make([]byte, 256*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("Stream read error for channel %s: %v", ch.Name, err)
			}
			break
		}
	}
}

func handleM3UImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, "Method not allowed", 405)
		return
	}

	// First try reading raw body (text/plain M3U data)
	// This must come before ParseMultipartForm which consumes the body
	ct := r.Header.Get("Content-Type")
	if !strings.Contains(ct, "multipart") && !strings.Contains(ct, "form") {
		data, _ := io.ReadAll(r.Body)
		if len(data) > 0 {
			channels := parseM3U(string(data))
			imported := 0
			for _, ch := range channels {
				lineup.Add(ch)
				imported++
			}
			state.Broadcast(map[string]interface{}{"type": "lineup_imported", "count": imported})
			jsonResponse(w, map[string]interface{}{
				"status":   "imported",
				"count":    imported,
				"channels": channels,
			})
			return
		}
	}

	// Check if file upload (multipart form)
	if err := r.ParseMultipartForm(64 << 20); err == nil { // 64MB max
		file, _, err := r.FormFile("file")
		if err == nil {
			defer file.Close()
			data, _ := io.ReadAll(file)
			channels := parseM3U(string(data))
			imported := 0
			for _, ch := range channels {
				lineup.Add(ch)
				imported++
			}
			state.Broadcast(map[string]interface{}{"type": "lineup_imported", "count": imported})
			jsonResponse(w, map[string]interface{}{
				"status":   "imported",
				"count":    imported,
				"channels": channels,
			})
			return
		}
	}

	// Check for URL import
	m3uURL := r.URL.Query().Get("url")
	if m3uURL != "" {
		if d, err := url.QueryUnescape(m3uURL); err == nil {
			m3uURL = d
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(m3uURL)
		if err != nil {
			jsonError(w, "Failed to fetch M3U: "+err.Error(), 502)
			return
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		channels := parseM3U(string(data))
		imported := 0
		for _, ch := range channels {
			lineup.Add(ch)
			imported++
		}
		state.Broadcast(map[string]interface{}{"type": "lineup_imported", "count": imported})
		jsonResponse(w, map[string]interface{}{
			"status":   "imported",
			"count":    imported,
			"channels": channels,
		})
		return
	}

	// Check for raw body
	data, _ := io.ReadAll(r.Body)
	if len(data) > 0 {
		channels := parseM3U(string(data))
		imported := 0
		for _, ch := range channels {
			lineup.Add(ch)
			imported++
		}
		state.Broadcast(map[string]interface{}{"type": "lineup_imported", "count": imported})
		jsonResponse(w, map[string]interface{}{
			"status":   "imported",
			"count":    imported,
			"channels": channels,
		})
		return
	}

	jsonError(w, "No M3U data provided. Send file upload, URL (?url=...), or raw M3U body.", 400)
}

func handleChannelGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	jsonResponse(w, map[string]interface{}{"groups": lineup.GetGroups()})
}

func handleChannelTune(w http.ResponseWriter, r *http.Request) {
	// POST /api/channels/tune?zone={zone_id}&channel={channel_id}
	if r.Method != "POST" {
		jsonError(w, "Method not allowed", 405)
		return
	}
	zoneID := r.URL.Query().Get("zone")
	chID := r.URL.Query().Get("channel")
	if zoneID == "" || chID == "" {
		jsonError(w, "zone and channel query params required", 400)
		return
	}

	state.mu.Lock()
	zone, ok := state.Zones[zoneID]
	if !ok {
		state.mu.Unlock()
		jsonError(w, "Zone not found", 404)
		return
	}

	ch, ok := lineup.Get(chID)
	if !ok {
		state.mu.Unlock()
		jsonError(w, "Channel not found", 404)
		return
	}

	// Set zone to this channel
	zone.SourceID = chID
	if zone.SourceID != chID {
		// Also update source to a channel-type source
		state.Sources[chID] = SourceInfo{
			ID:    chID,
			Name:  ch.Name,
			Icon:  "📺",
			Color: "#f59e0b",
		}
		zone.SourceID = chID
	}
	state.mu.Unlock()

	lineup.IncrementViewers(chID)

	state.Broadcast(map[string]interface{}{
		"type":    "zone_update",
		"zone":    zone,
		"channel": ch,
	})

	jsonResponse(w, map[string]interface{}{
		"status":  "tuned",
		"zone":    zoneID,
		"channel": ch,
	})
}
