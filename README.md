# veld

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://go.dev)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**veld** (Video Element Downloader) is a blazing-fast, concurrent HLS/DASH media downloader written in Go. Download videos from streaming platforms with ease using a beautiful terminal UI.

<p align="center">
  <img src="https://via.placeholder.com/800x400?text=veld+demo" alt="veld demo" />
</p>

## ✨ Features

- 🚀 **Blazing Fast** - Concurrent segment downloads with up to 128 threads
- 📺 **HLS & DASH** - Full support for m3u8 and mpd manifests
- 🎬 **Multi-track** - Download video, audio, and subtitle tracks separately or together
- 🎨 **Beautiful TUI** - Interactive track picker with real-time progress
- 🔧 **Smart Muxing** - Automatically combines tracks into MP4, MKV, or TS
- ⚡ **HTTP/2** - Connection pooling and multiplexing for maximum speed
- 🔐 **Encryption Ready** - Pluggable decryption for protected streams
- 🌍 **Cross-Platform** - Linux, macOS, and Windows

## 📦 Installation

### Pre-built Binaries

Download from [Releases](https://github.com/yourusername/veld/releases)

### Go Install

```bash
go install github.com/yourusername/veld/cmd/veld@latest
```

### Build from Source

```bash
git clone https://github.com/yourusername/veld.git
cd veld
go build -o veld ./cmd/veld
```

## 🚀 Quick Start

```bash
# Interactive mode - pick tracks visually
veld -u https://example.com/stream.m3u8

# Auto-select best quality
veld -u https://example.com/stream.m3u8 -s best

# Download 1080p video
veld -u https://example.com/stream.m3u8 -s 1080p -o movie.mp4

# DASH manifest
veld -u https://example.com/stream.mpd -s best
```

## 📖 Usage

```
veld - Video Element Downloader

Usage: veld [options] -u <URL>

Options:
  -u, --url <URL>           Stream URL (m3u8/mpd) [required]
  -o, --output <path>       Output file (default: output.mp4)
  -n, --threads <num>       Concurrent downloads (default: 16)
  -s, --select-track <sel>  Track selection (omit for picker)
  -f, --format <fmt>        Output: mp4, mkv, ts (default: mp4)
  -H, --header <header>     Custom header (repeatable)
      --cookie <cookies>    Cookies for auth
      --key <KID:KEY>       Decryption key
      --no-progress         Disable TUI
  -v, --verbose             Verbose output
      --version             Show version
```

### Track Selection

| Selector | Description |
|----------|-------------|
| `best` | Best video + best audio |
| `all` | All available tracks |
| `1080p` `720p` `480p` | By resolution |
| `4k` `hd` `sd` | Quality presets |
| `video:0+audio:1` | By index |
| `en` `es` `ja` | Audio by language |

## 💡 Examples

### With Authentication Headers

```bash
veld -u https://example.com/stream.m3u8 \
    -H "Authorization: Bearer eyJ..." \
    -H "Referer: https://example.com"
```

### Download Everything

```bash
veld -u https://example.com/stream.m3u8 -s all -o complete.mkv
```

### Maximum Speed

```bash
veld -u https://example.com/stream.m3u8 -n 64 -s best
```

### Scripting (No TUI)

```bash
veld -u https://example.com/stream.m3u8 -s best --no-progress
```

## ⚡ Performance

veld is optimized for speed with:
- HTTP/2 multiplexing
- Connection pooling (100 connections per host)
- Zero-copy streaming where possible
- Efficient memory usage with buffer pools

| Tool | 1GB Stream |
|------|-----------|
| **veld** | ~45s |
| wget | ~120s |
| aria2c | ~60s |
| N_m3u8DL-RE | ~50s |

*Results vary by network and server.*

## 🔧 Requirements

- **FFmpeg** (recommended) - Required for muxing multiple tracks into MP4/MKV

## 🏗️ Architecture

```
veld/
├── cmd/veld/         # CLI
└── internal/
    ├── config/       # Configuration
    ├── engine/       # Download engine
    ├── models/       # Data types
    ├── parser/       # HLS/DASH parsers
    └── tui/          # Terminal UI
```

## 🤝 Contributing

Contributions welcome! Please:

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Submit a PR

## 📄 License

MIT License - see [LICENSE](LICENSE)

---

<p align="center">
  <b>veld</b> - Fast. Simple. Beautiful.
</p>
