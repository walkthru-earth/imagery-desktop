# Walkthru Earth - Imagery Desktop

A cross-platform desktop application for downloading, visualizing, and exporting satellite imagery from Google Earth and Esri Wayback archives.

## Features

- **Multi-Source Imagery** - Access Google Earth historical imagery and Esri Wayback archives
- **Interactive Map** - MapLibre GL-based viewer with temporal slider for date selection
- **Batch Downloads** - Download imagery for custom bounding boxes with configurable zoom levels
- **Video Export** - Create timelapse videos from historical imagery sequences
- **Persistent Cache** - OGC-compliant tile cache with cross-session persistence
- **Cross-Platform** - Native desktop application for macOS, Windows, and Linux

## Architecture

```mermaid
graph TB
    subgraph "Frontend (React/TypeScript)"
        UI[User Interface]
        ML[MapLibre GL Map]
        API[API Service Layer]
    end

    subgraph "Backend (Go)"
        APP[Wails App Controller]
        TS[Tile Server - Both Providers]
        CACHE[Persistent Cache]
        DL[Download Manager]
        VE[Video Exporter]
        TQ[Task Queue]
        RL[Rate Limit Handler]
    end

    subgraph "External Services"
        GE[Google Earth API]
        ES[Esri Wayback API]
    end

    subgraph "Storage"
        DISK[File System]
        DB[(Cache Index)]
    end

    UI --> API
    ML --> API
    API --> APP
    APP --> TS
    APP --> DL
    APP --> VE
    APP --> TQ
    DL --> RL
    DL --> CACHE
    TS --> CACHE
    CACHE --> DB
    CACHE --> DISK
    DL --> GE
    DL --> ES
    VE --> DISK
```

## Technology Stack

| Layer | Technologies |
|-------|-------------|
| Frontend | React 18, TypeScript, MapLibre GL, Tailwind CSS v4, shadcn/ui, Vite |
| Backend | Go 1.21+, Wails v2.11, FFmpeg, Protocol Buffers |
| Storage | OGC ZXY tile cache, JSON metadata, GeoTIFF export |

## Data Flow

### Tile Caching

```mermaid
sequenceDiagram
    participant UI as User Interface
    participant API as API Layer
    participant TS as Tile Server
    participant Cache as Persistent Cache
    participant Disk as File System
    participant Provider as Google Earth/Esri

    UI->>API: Request tile (z, x, y, date)
    API->>TS: GET /google-earth/{date}/{z}/{x}/{y}

    TS->>Cache: Get(provider:z:x:y:date)

    alt Cache Hit
        Cache->>Disk: Read tile from disk
        Disk-->>Cache: Return tile data
        Cache-->>TS: Return cached tile
    else Cache Miss
        TS->>Provider: Fetch tile
        Provider-->>TS: Return tile data
        TS->>Cache: Set(provider, z, x, y, date, data)
        Cache->>Disk: Write tile to OGC structure
        Cache->>Disk: Update cache_index.json
    end

    TS-->>API: Return tile
    API-->>UI: Display on map
```

### Download & Export

```mermaid
sequenceDiagram
    participant UI as User Interface
    participant API as API Layer
    participant DL as Download Manager
    participant RL as Rate Limit Handler
    participant Cache as Persistent Cache
    participant VE as Video Exporter
    participant TQ as Task Queue

    UI->>API: Request download (bbox, zoom, dates)
    API->>DL: DownloadImageryRange()

    loop For each tile
        DL->>RL: Check if rate limited
        alt Rate Limited
            RL-->>DL: Pause (wait for retry)
        else Not Rate Limited
            DL->>Cache: Check cache first
            alt Cache Hit
                Cache-->>DL: Return cached tile
            else Cache Miss
                DL->>Provider: Fetch tile
                alt Success
                    Provider-->>DL: Tile data
                    DL->>Cache: Store tile
                else Rate Limit (403/429)
                    Provider-->>DL: Rate limit error
                    DL->>RL: Record rate limit
                    RL->>UI: Emit rate-limit-detected event
                end
            end
        end
    end

    alt Export as Video
        UI->>API: ExportTimelapseVideo()
        API->>TQ: Add export task
        TQ->>VE: Process frames
        VE->>FFmpeg: Encode video
        FFmpeg-->>VE: Video file
        VE-->>TQ: Task complete
        TQ->>UI: Emit task-complete event
    end
```

### Rate Limit Handling

```mermaid
stateDiagram-v2
    [*] --> Normal: Initial State
    Normal --> RateLimited: HTTP 403/429/509

    RateLimited --> Waiting: Schedule Retry (5min)
    Waiting --> Retrying: Timer Expires

    Retrying --> Normal: Success (200 OK)
    Retrying --> RateLimited: Still Limited

    RateLimited --> Backoff: Increment Retry
    Backoff --> Waiting: Wait (10min, 15min, 20min, 30min)

    RateLimited --> Manual: User clicks "Retry Now"
    Manual --> Normal: Success
    Manual --> RateLimited: Still Limited

    note right of RateLimited
        Retry intervals:
        1st: 5 min
        2nd: 10 min
        3rd: 15 min
        4th: 20 min
        5th+: 30 min
    end note
```

## Cache Structure

OGC ZXY-compliant directory structure compatible with GeoServer, QGIS, and GDAL:

```
~/.walkthru-earth/imagery-desktop/cache/
├── cache_index.json              # Metadata index (LRU, TTL, sizes)
├── google_earth/
│   └── {date}/                   # e.g., 2024-12-31
│       └── {z}/{x}/{y}.jpg       # OGC ZXY structure
└── esri_wayback/
    └── {date}/
        └── {z}/{x}/{y}.jpg
```

| Feature | Value |
|---------|-------|
| Max Size | 500 MB (configurable) |
| TTL | 90 days (configurable) |
| Eviction | LRU when exceeding size limit |
| Persistence | Survives app restarts |

## Installation

Download the latest release from [walkthru.earth/software/imagery-desktop](https://walkthru.earth/software/imagery-desktop).

### macOS

The app is not signed with an Apple Developer certificate. After downloading:

```bash
# Remove quarantine attribute
xattr -cr /path/to/imagery-desktop.app

# Or if you moved it to Applications:
xattr -cr /Applications/imagery-desktop.app
```

If you see "damaged and can't be opened", this command will fix it.

### Windows

Windows SmartScreen may show "Windows protected your PC". Click **More info** → **Run anyway**.

## Development

### Prerequisites

- Go 1.21+
- Node.js 18+
- Wails CLI v2.11.0+

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### Run

```bash
wails dev
```

### Build

```bash
# Current platform
wails build

# All platforms
./scripts/build-all.sh
```

## Configuration

Settings stored in `~/.walkthru-earth/imagery-desktop/settings/settings.json`:

| Setting | Default | Description |
|---------|---------|-------------|
| `downloadPath` | `~/Downloads/imagery` | Where downloads are saved |
| `cacheMaxSizeMB` | `500` | Maximum cache size |
| `cacheTTLDays` | `90` | Cache expiration |
| `defaultZoom` | `15` | Default map zoom level |
| `defaultSource` | `esri` | Default imagery source |

## Project Structure

```
├── app.go                    # Main Wails app controller
├── main.go                   # Entry point
├── frontend/
│   └── src/
│       ├── components/       # UI components (shadcn/ui)
│       ├── hooks/            # React hooks
│       ├── services/         # API service layer
│       └── contexts/         # React contexts
├── internal/
│   ├── cache/                # Persistent tile cache
│   ├── downloads/            # Download orchestration
│   │   ├── esri/             # Esri downloader
│   │   └── googleearth/      # Google Earth downloader
│   ├── handlers/tileserver/  # Unified tile server
│   ├── ratelimit/            # Rate limit handling
│   ├── taskqueue/            # Background tasks
│   └── video/                # Video export
└── pkg/geotiff/              # GeoTIFF encoding
```

## Documentation

- [ARCHITECTURE.md](docs/ARCHITECTURE.md) - Detailed system architecture and edge cases
- [GOOGLE_EARTH_API_NOTES.md](docs/GOOGLE_EARTH_API_NOTES.md) - Google Earth API reference (Flatfile & RT/Earth APIs)
- [EPOCH_FIX_SUMMARY.md](docs/EPOCH_FIX_SUMMARY.md) - Historical imagery epoch handling

## Legal Notice

This project is for **educational purposes only**.

### Software License

The code is licensed under [CC BY 4.0](LICENSE). You may share and adapt with attribution to Walkthru Earth.

### Imagery Copyright

Satellite imagery remains property of the respective providers:
- **Esri Wayback** - © Esri and its data providers
- **Google Earth** - © Google and its data providers

Users are responsible for complying with provider terms of service.

---

**Walkthru Earth** | [hi@walkthru.earth](mailto:hi@walkthru.earth)
