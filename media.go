package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ─── Media Types ───────────────────────────────────────────────

var (
	audioExts = map[string]bool{
		".mp3": true, ".wav": true, ".flac": true, ".aac": true,
		".ogg": true, ".wma": true, ".m4a": true, ".opus": true,
		".m4b": true, ".aiff": true, ".alac": true,
	}
	videoExts = map[string]bool{
		".mp4": true, ".mkv": true, ".avi": true, ".mov": true,
		".wmv": true, ".flv": true, ".webm": true, ".m4v": true,
		".ts": true, ".vob": true, ".ogv": true, ".3gp": true,
		".mpeg": true, ".mpg": true, ".divx": true, ".f4v": true,
	}
	mediaExts = mergeMediaExts(audioExts, videoExts)
)

func mergeMediaExts(a, b map[string]bool) map[string]bool {
	m := make(map[string]bool, len(a)+len(b))
	for k, v := range a { m[k] = v }
	for k, v := range b { m[k] = v }
	return m
}

type MediaFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"`
	Size      int64  `json:"size"`
	SizeHuman string `json:"size_human"`
	Ext       string `json:"ext"`
	Folder    string `json:"folder"`
}

type FolderInfo struct {
	Path  string      `json:"path"`
	Files []MediaFile `json:"files"`
	Count int         `json:"count"`
}

// ─── Media Cache ───────────────────────────────────────────────

type MediaCache struct {
	mu       sync.RWMutex
	folders  map[string]FolderInfo
	total    int
	lastScan time.Time
	scanning bool
}

func (c *MediaCache) Get() (map[string]FolderInfo, int, time.Time, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	f := make(map[string]FolderInfo, len(c.folders))
	for k, v := range c.folders { f[k] = v }
	return f, c.total, c.lastScan, c.scanning
}

func (c *MediaCache) Update(folders map[string]FolderInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.folders = folders
	c.total = 0
	for _, f := range folders { c.total += f.Count }
	c.lastScan = time.Now()
	c.scanning = false
}

func (c *MediaCache) SetScanning(s bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scanning = s
}

// ─── Scanner ───────────────────────────────────────────────────

func scanFolder(root string, maxFiles int) []MediaFile {
	var files []MediaFile
	count := 0
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if info.IsDir() { return nil }
		if count >= maxFiles { return filepath.SkipDir }
		ext := strings.ToLower(filepath.Ext(path))
		if !mediaExts[ext] { return nil }
		ft := "audio"
		if videoExts[ext] { ft = "video" }
		files = append(files, MediaFile{
			Name: info.Name(), Path: path, Type: ft,
			Size: info.Size(), SizeHuman: formatSize(info.Size()),
			Ext: ext, Folder: filepath.Base(filepath.Dir(path)),
		})
		count++
		return nil
	})
	return files
}

func getMediaFolders() []string {
	home, _ := os.UserHomeDir()
	folders := []string{"Music", "Videos", "Downloads", "Desktop", "Documents"}
	var result []string
	for _, f := range folders {
		p := filepath.Join(home, f)
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			result = append(result, p)
		}
	}
	return result
}

func getUSBDrives() []string {
	var drives []string
	out, _ := exec.Command("powershell", "-NoProfile", "-Command",
		"Get-WmiObject Win32_LogicalDisk | Where-Object { $_.DriveType -eq 2 } | ForEach-Object { Write-Output $_.DeviceID }").Output()
	for _, line := range strings.Split(string(out), "\n") {
		d := strings.TrimSpace(line)
		if d != "" { drives = append(drives, d+`\`) }
	}
	return drives
}

func RunFullScan() {
	state.MediaCache.SetScanning(true)
	state.Broadcast(map[string]interface{}{"type": "media_scan_started"})
	folders := getMediaFolders()
	result := make(map[string]FolderInfo)
	total := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, fp := range folders {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			files := scanFolder(p, 500)
			if len(files) == 0 { return }
			sort.Slice(files, func(i, j int) bool {
				return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
			})
			mu.Lock()
			result[filepath.Base(p)] = FolderInfo{Path: p, Files: files, Count: len(files)}
			total += len(files)
			mu.Unlock()
			state.Broadcast(map[string]interface{}{
				"type": "media_scan_progress", "folder": filepath.Base(p),
				"count": len(files), "total": total, "scanning": true,
			})
		}(fp)
	}
	wg.Wait()
	state.MediaCache.Update(result)
	state.Broadcast(map[string]interface{}{"type": "media_scan_complete", "total": total, "folders": len(result)})
}

// ─── Media Scan Handlers ───────────────────────────────────────

func handleMediaScan(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p != "" {
		p = strings.ReplaceAll(p, "/", `\`)
		files := scanFolder(p, 500)
		sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
		jsonResponse(w, map[string]interface{}{"files": files, "count": len(files)})
		return
	}
	folders, total, lastScan, scanning := state.MediaCache.Get()
	if scanning {
		jsonResponse(w, map[string]interface{}{"files": []MediaFile{}, "count": 0, "scanning": true, "lastScan": lastScan.Unix()})
		return
	}
	if total == 0 || time.Since(lastScan) > 5*time.Minute {
		go RunFullScan()
		if total > 0 {
			var all []MediaFile
			for _, f := range folders { all = append(all, f.Files...) }
			sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name) })
			jsonResponse(w, map[string]interface{}{"files": all, "count": len(all), "cached": true, "scanning": true})
			return
		}
		jsonResponse(w, map[string]interface{}{"files": []MediaFile{}, "count": 0, "scanning": true, "message": "Scan started..."})
		return
	}
	var all []MediaFile
	for _, f := range folders { all = append(all, f.Files...) }
	sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name) })
	jsonResponse(w, map[string]interface{}{"files": all, "count": len(all), "cached": true})
}

func handleMediaScanGrouped(w http.ResponseWriter, r *http.Request) {
	folders, total, lastScan, scanning := state.MediaCache.Get()
	if scanning {
		jsonResponse(w, map[string]interface{}{"folders": map[string]FolderInfo{}, "total": 0, "scanning": true, "lastScan": lastScan.Unix()})
		return
	}
	if total == 0 || time.Since(lastScan) > 5*time.Minute {
		go RunFullScan()
		if total > 0 {
			jsonResponse(w, map[string]interface{}{"folders": folders, "total": total, "cached": true, "scanning": true})
			return
		}
		jsonResponse(w, map[string]interface{}{"folders": map[string]FolderInfo{}, "total": 0, "scanning": true, "message": "Scan started..."})
		return
	}
	jsonResponse(w, map[string]interface{}{"folders": folders, "total": total})
}

func handleMediaScanFolder(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" { jsonError(w, "path required", 400); return }
	if d, err := url.QueryUnescape(p); err == nil { p = d }
	p = strings.ReplaceAll(p, "/", `\`)
	if info, err := os.Stat(p); err != nil || !info.IsDir() {
		jsonResponse(w, map[string]interface{}{"error": "Folder not found: " + p, "files": []MediaFile{}, "count": 0, "folder": p})
		return
	}
	files := scanFolder(p, 500)
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name) })
	jsonResponse(w, map[string]interface{}{"files": files, "count": len(files), "folder": p})
}

func handleMediaScanUSB(w http.ResponseWriter, r *http.Request) {
	drives := getUSBDrives()
	var all []MediaFile
	for _, d := range drives {
		files := scanFolder(d, 500)
		for i := range files { files[i].Folder = d + " " + files[i].Folder }
		all = append(all, files...)
	}
	sort.Slice(all, func(i, j int) bool { return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name) })
	jsonResponse(w, map[string]interface{}{"files": all, "count": len(all)})
}

func handleMediaRefresh(w http.ResponseWriter, r *http.Request) {
	go RunFullScan()
	jsonResponse(w, map[string]string{"status": "scan_started"})
}

func handleMediaStatus(w http.ResponseWriter, r *http.Request) {
	_, total, lastScan, scanning := state.MediaCache.Get()
	jsonResponse(w, map[string]interface{}{"total": total, "lastScan": lastScan.Unix(), "scanning": scanning})
}

// ─── Media File Serving ────────────────────────────────────────

func mimeForExt(ext string) string {
	switch ext {
	case ".mp3": return "audio/mpeg"
	case ".mp4", ".m4v": return "video/mp4"
	case ".mkv": return "video/x-matroska"
	case ".webm": return "video/webm"
	case ".avi": return "video/x-msvideo"
	case ".mov": return "video/quicktime"
	case ".ts": return "video/mp2t"
	case ".flv": return "video/x-flv"
	case ".wmv": return "video/x-ms-wmv"
	case ".ogg", ".ogv": return "video/ogg"
	case ".flac": return "audio/flac"
	case ".wav": return "audio/wav"
	case ".aac": return "audio/aac"
	case ".m4a": return "audio/mp4"
	case ".opus": return "audio/opus"
	case ".wma": return "audio/x-ms-wma"
	default: return "application/octet-stream"
	}
}

func handleMediaPlay(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" { jsonError(w, "Path required", 400); return }
	if d, err := url.QueryUnescape(p); err == nil { p = d }
	p = strings.ReplaceAll(p, "/", `\`)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() { jsonError(w, "File not found: "+p, 404); return }
	ext := strings.ToLower(filepath.Ext(p))
	w.Header().Set("Content-Type", mimeForExt(ext))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	http.ServeFile(w, r, p)
}

func handleMediaInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { jsonError(w, "Method not allowed", 405); return }
	var data map[string]interface{}
	json.NewDecoder(r.Body).Decode(&data)
	p, _ := data["path"].(string)
	info, err := os.Stat(p)
	if err != nil { jsonError(w, "File not found", 404); return }
	ext := strings.ToLower(filepath.Ext(p))
	mimeType := mimeForExt(ext)
	duration := 0.0
	if out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", p).Output(); err == nil {
		var probeData map[string]interface{}
		if json.Unmarshal(out, &probeData) == nil {
			if format, ok := probeData["format"].(map[string]interface{}); ok {
				if d, ok := format["duration"].(string); ok { fmt.Sscanf(d, "%f", &duration) }
			}
		}
	}
	durStr := "Unknown"
	if duration > 0 {
		mins := int(duration) / 60; secs := int(duration) % 60
		durStr = fmt.Sprintf("%d:%02d", mins, secs)
	}
	ft := "audio"; if videoExts[ext] { ft = "video" }
	jsonResponse(w, map[string]interface{}{
		"name": filepath.Base(p), "path": p, "size": info.Size(),
		"size_human": formatSize(info.Size()), "mime": mimeType,
		"duration": duration, "duration_human": durStr, "type": ft,
	})
}

// ─── YouTube Player ────────────────────────────────────────────

func findYTDLP() string {
	if p, err := exec.LookPath("yt-dlp"); err == nil { return p }
	candidates := []string{
		`C:\Users\RJ\AppData\Local\Programs\Python310\Scripts\yt-dlp.exe`,
		`C:\Users\RJ\AppData\Roaming\Python\Python310\Scripts\yt-dlp.exe`,
		`C:\Users\RJ\AppData\Local\Microsoft\WindowsApps\yt-dlp.exe`,
		`C:\ProgramData\chocolatey\bin\yt-dlp.exe`,
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil { return c }
	}
	return ""
}

func getYouTubeInfo(videoID string) (map[string]interface{}, error) {
	result := map[string]interface{}{"id": videoID, "title": "YouTube: " + videoID}
	ytDLP := findYTDLP()
	if ytDLP == "" { return result, nil }
	cmd := exec.Command(ytDLP, "--dump-json", "--no-download", "--no-warnings",
		"https://www.youtube.com/watch?v="+videoID)
	out, err := cmd.Output()
	if err != nil { return result, nil }
	var info map[string]interface{}
	if json.Unmarshal(out, &info) != nil { return result, nil }
	if t, ok := info["title"].(string); ok { result["title"] = t }
	if d, ok := info["duration"].(float64); ok { result["duration"] = int(d) }
	if th, ok := info["thumbnail"].(string); ok { result["thumbnail"] = th }
	if u, ok := info["uploader"].(string); ok { result["uploader"] = u }
	return result, nil
}

func handleYouTubeInfo(w http.ResponseWriter, r *http.Request) {
	videoID := r.URL.Query().Get("id")
	if videoID == "" { jsonError(w, "Video ID required", 400); return }
	info, _ := getYouTubeInfo(videoID)
	jsonResponse(w, info)
}

func handleYouTubeStream(w http.ResponseWriter, r *http.Request) {
	videoID := r.URL.Query().Get("id")
	if videoID == "" { jsonError(w, "Video ID required", 400); return }
	http.Redirect(w, r, "/api/youtube/direct?id="+videoID, http.StatusFound)
}

func handleYouTubeDirect(w http.ResponseWriter, r *http.Request) {
	videoID := r.URL.Query().Get("id")
	if videoID == "" { jsonError(w, "Video ID required", 400); return }
	ytDLP := findYTDLP()
	if ytDLP == "" {
		http.Redirect(w, r, "https://www.youtube.com/embed/"+videoID+"?autoplay=1", http.StatusFound)
		return
	}
	cmd := exec.Command(ytDLP, "-g", "--no-warnings",
		"-f", "best[height<=1080][ext=mp4]/best[height<=720][ext=mp4]/best[height<=720]/best",
		"https://www.youtube.com/watch?v="+videoID)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("yt-dlp error for %s: %v", videoID, err)
		http.Redirect(w, r, "https://www.youtube.com/embed/"+videoID+"?autoplay=1", http.StatusFound)
		return
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		http.Redirect(w, r, "https://www.youtube.com/embed/"+videoID+"?autoplay=1", http.StatusFound)
		return
	}
	lines := strings.Split(raw, "\n")
	streamURL := strings.TrimSpace(lines[0])
	client := &http.Client{
		Timeout: 60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return nil },
	}
	req, _ := http.NewRequest("GET", streamURL, nil)
	if rh := r.Header.Get("Range"); rh != "" { req.Header.Set("Range", rh) }
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Stream fetch error: %v", err)
		http.Redirect(w, r, "https://www.youtube.com/embed/"+videoID+"?autoplay=1", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	for k, v := range resp.Header {
		if k == "Transfer-Encoding" || k == "Connection" || k == "Alt-Svc" { continue }
		for _, vv := range v { w.Header().Add(k, vv) }
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if w.Header().Get("Content-Type") == "" { w.Header().Set("Content-Type", "video/mp4") }
	w.WriteHeader(resp.StatusCode)
	buf := make([]byte, 256*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			w.Write(buf[:n])
			if f, ok := w.(http.Flusher); ok { f.Flush() }
		}
		if err != nil { break }
	}
}
