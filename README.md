# HotelMP3 — Multi-Zone Hotel AV Control System

[![CI](https://github.com/lamanaict/hotelmp3/actions/workflows/ci.yml/badge.svg)](https://github.com/lamanaict/hotelmp3/actions/workflows/ci.yml)
[![Release](https://github.com/lamanaict/hotelmp3/actions/workflows/release.yml/badge.svg)](https://github.com/lamanaict/hotelmp3/releases)

A portable, single-binary AV control system for hotels. Go backend with real-time WebSocket sync and a Glass UI frontend.

## Features

- **Multi-zone control** — Independent volume, power, and source per room/zone
- **Real-time sync** — WebSocket-based state synchronization across all connected clients
- **Media library** — Concurrent media scanner with folder browser and playlist
- **YouTube playback** — Stream extraction via yt-dlp proxy
- **Zone groups** — Create/join/unjoin zones for synchronized playback
- **Glass UI** — Spatial UI with HUD overlay, glassmorphism sidebar, cream backgrounds
- **Single binary** — One executable, no dependencies, runs from USB

## Quick Start

### Run from source
```bash
go build -ldflags="-s -w" -o HotelMp3.exe .
./HotelMp3.exe
```

### Run from release
Download the latest release for your platform from the [Releases](https://github.com/lamanaict/hotelmp3/releases) page.

Then:
```bash
./HotelMp3        # Linux/macOS
HotelMp3.exe      # Windows
```

Open `http://localhost:8000` in your browser.

## Options

| Flag | Default | Description |
|------|---------|-------------|
| `-port` | `8000` | Server port |
| `-no-browser` | `false` | Don't auto-open browser |

## Development

```bash
# Build
go build -v ./...

# Test
go test -v ./...

# Build optimized binary
go build -ldflags="-s -w" -o HotelMp3.exe .
```

## Release

Create a tag to trigger an automated release:
```bash
git tag v1.0.0
git push origin v1.0.0
```

This builds binaries for Linux, Windows, and macOS (amd64 + arm64) and creates a GitHub Release.

## Architecture

```
HotelMp3.exe
├── Go backend (net/http + gorilla/websocket)
│   ├── HTTP server (static files, API routes)
│   ├── WebSocket hub (real-time broadcast)
│   ├── Media scanner (concurrent filepath.Walk)
│   └── YouTube proxy (yt-dlp stream extraction)
└── Static frontend
    ├── Glass UI (CSS glassmorphism + HUD)
    ├── Zone cards + volume controls
    ├── EQ mixer + media player
    └── YouTube player
```

## License

MIT
