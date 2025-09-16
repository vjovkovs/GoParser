# GoParser

GoParser is a collection of command-line tools written in Go for crawling and parsing long-form web content (chapters, articles) and converting text to audio via a Kokoro-compatible gRPC TTS server. It includes:

- A web parser and runner (`cmd/gwp`) that crawls a start URL, extracts chapter content, and emits per-chapter artifacts (EPUB, PDF, TXT).
- A simple Kokoro-compatible TTS gRPC server stub (`cmd/kokoro-server`) that uses `espeak-ng` for quick local testing.
- A TTS client CLI (`cmd/tts`) that connects to a Kokoro gRPC server (or the included stub) and synthesizes text files into audio chunks and optional combined outputs.

This README covers how to build, run, and contribute.

## Table of contents

- What it does
- Requirements
- Quickstart
  - Build
  - Run the sample Kokoro server (dev)
  - Parse a site (gwp)
  - Convert text -> audio (tts)
- Configuration
- Internals & architecture
- Contributing
- License

## What it does

GoParser focuses on two main workflows:

- Crawl and parse multi-page stories or serialized books, normalize the content into chapters, and save per-chapter artifacts (HTML/JSON, EPUB, PDF, TXT).
- Convert plaintext chapters into audio files by communicating with a Kokoro-compatible TTS gRPC server. A local test server using `espeak-ng` is included for convenience.

Use cases: converting web serials to ebooks, archiving articles, or generating audiobooks from parsed chapter text.

## Requirements

- Go 1.25 or newer (module uses go 1.25)
- A Kokoro-compatible gRPC TTS server for high-quality synthesis. For local testing you can run the included `cmd/kokoro-server` which uses `espeak-ng`.
- Optional tools for audio post-processing: `ffmpeg` or `lame` when combining/encoding to MP3.

On Windows, ensure developer tools and `espeak-ng` (and optionally `ffmpeg`/`lame`) are on your PATH.

## Quickstart

Build the project and run the commands below from the repository root.

Build (tidy + build all binaries):

```powershell
go mod tidy
go build ./...
```

This will produce binaries under the module's packages (or you can run via `go run`). The repo also ships a `tts.exe` (prebuilt) in the root for convenience.

### Run the local Kokoro test server (dev)

The included `cmd/kokoro-server` implements the same gRPC interface expected by the `tts` client and uses `espeak-ng` to synthesize audio. It's suitable for local testing.

```powershell
# run the server (uses espeak-ng behind the scenes)
go run ./cmd/kokoro-server --listen "localhost:50051" --voice "en-us" --speed 170
```

The server listens on the given address (default `localhost:50051`) and streams raw audio chunks over gRPC.

### Parse a site into chapters (gwp)

Use the `gwp` runner to crawl and extract chapters from a start URL.

```powershell
go run ./cmd/gwp --url "https://example.com/first-chapter" --out out --fmt epub --title "My Book Title" --author "Author Name" --max 10
```

Key flags:

- `--url` (required): Start URL to parse.
- `--out`: Output directory (default `out`).
- `--max`: Max pages to crawl.
- `--fmt`: Final artifact format (`epub`, `pdf`, `txt`) — requires `--title` to produce a full book.
- `--chapter-fmt`: Comma-separated per-chapter outputs (e.g. `epub,pdf,txt`).
- `--save-each` (default true): persist each chapter to the output store as the crawler runs.

Per-chapter files are saved under `out/chapters/<slug>/...` and final book artifacts go under `out/epub`, `out/pdf`, or `out/txt`.

### Convert text files to audio (tts)

The `tts` CLI reads one or more `.txt` files (or a directory) and synthesizes audio by talking to a Kokoro gRPC server.

```powershell
# synthesize a single file (requires Kokoro server running at localhost:50051)
go run ./cmd/tts --in "path\to\chapter.txt" --out "out\audio" --final mp3 --format wav --voice af_heart
```

Common flags:

- `--in` (required): Path to `.txt` or directory of `.txt` files.
- `--out`: Output root directory (default `out/audio`).
- `--kokoro-addr`: Kokoro gRPC address (default `localhost:50051`).
- `--kokoro-insecure`: Use plaintext (true by default, for local dev).
- `--format`: Requested server audio format (`wav` recommended).
- `--final`: Final combined output: `mp3`, `wav`, or `none`.
- `--list-voices`: Query the server for available voices and exit.

Notes:

- When the server returns WAV chunks, `tts` can combine them into a single WAV and optionally encode to MP3 (needs `lame` or `ffmpeg`).
- When the server returns MP3s already, `tts` will attempt to concatenate them via `ffmpeg` if available.

## Configuration

Parser defaults are in `configs/default.yaml` and mirror the internal parser config. Key options include selectors used for content extraction, next-page link texts, minimum text length for a chapter, and polite delays.

Example `configs/default.yaml`:

```yaml
selectors:
  - "article .entry-content"
  - ".entry-content"
  - ".post-content"
  - "article"
  - ".chapter-content"
next_texts: ["Next", "Next Chapter", "Next Page", ">>", "»", "→"]
min_text_len: 800
polite_delay_sec: 1
scene_break_patterns:
  - "^[-_*]{3,}$"
  - "^\\* *\\* *\\* *$"
  - "^—+$"
  - "^†$"
scene_break_style: "hr"
consecutive_br_threshold: 3
```

## Internals & architecture

- `cmd/gwp`: CLI for crawling/parsing and building book artifacts. It orchestrates the fetch client, parser, storage, and writer components.
- `cmd/tts`: CLI that uses `internal/tts.KokoroEngine` or the included local `tts.Engine` wrapper around `espeak-ng`.
- `cmd/kokoro-server`: Minimal gRPC server implementing the project's `api/tts/v1` protobuf service for testing.
- `internal/app`, `internal/fetch`, `internal/model`, `internal/storage`, `internal/writer`: core packages driving crawling, parsing, persisting, and writing EPUB/PDF/TXT outputs.

TTS gRPC API: Protobuf definitions live under `api/tts/v1` and expose a `Synthesize` server-streaming RPC which streams raw audio chunks.

## Contributing

Contributions are welcome. A suggested workflow:

1. Fork the repository.
2. Create a feature branch.
3. Run `go test ./...` and add tests when appropriate.
4. Open a pull request describing the change.

Please add tests for new parsing heuristics or any change affecting output formats.

## License

This repository does not include an explicit LICENSE file. If you intend to publish this project, add a license (for example MIT) to make terms clear. The project includes references and samples that may include third-party assets; please verify licenses of bundled assets in `testdata/` before redistributing those files.

## Notes & troubleshooting

- If `espeak-ng` is not found, the included test server and local TTS engine will fail; install it and add it to PATH.
- For MP3 encoding, install `lame` or `ffmpeg`. On Windows, ensure the `.exe` is on PATH.
- If you want production TLS for Kokoro, update `internal/tts` to provide TLS credentials instead of the insecure option.

If you'd like, I can also open a small PR that adds a LICENSE (MIT) and a tiny usage example script.

---
Generated README based on the repository contents and command entrypoints.
