package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/vjovkovs/goparser/internal/app"
	"github.com/vjovkovs/goparser/internal/fetch"
	"github.com/vjovkovs/goparser/internal/model"
	"github.com/vjovkovs/goparser/internal/storage"
	"github.com/vjovkovs/goparser/internal/util"
	"github.com/vjovkovs/goparser/internal/writer"
)

func ensureSubdir(base, name string) string {
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("make %s dir: %v", name, err)
	}
	return dir
}

func parseFormats(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		f := strings.ToLower(strings.TrimSpace(p))
		if f == "" || seen[f] {
			continue
		}
		switch f {
		case "epub", "pdf", "txt":
			out = append(out, f)
			seen[f] = true
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return strings.TrimSpace(b)
}

func writePerChapterArtifacts(ch model.Chapter, outDir string, fmts []string, defaultAuthor string) error {
	// **Unified slug** (same as storage.Save)
	base := util.ChapterSlug(ch)

	chapRoot := filepath.Join(outDir, "chapters", base)

	for _, f := range fmts {
		switch f {
		case "pdf":
			dir := filepath.Join(chapRoot, "pdf")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			out := filepath.Join(dir, base+".pdf")
			if err := writer.WritePDF([]model.Chapter{ch}, out); err != nil {
				return fmt.Errorf("pdf %s: %w", ch.Title, err)
			}
			log.Printf("Wrote chapter PDF: %s", out)

		case "txt":
			dir := filepath.Join(chapRoot, "txt")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			out := filepath.Join(dir, base+".txt")
			if err := writer.WriteTXT([]model.Chapter{ch}, out); err != nil {
				return fmt.Errorf("txt %s: %w", ch.Title, err)
			}
			log.Printf("Wrote chapter TXT: %s", out)

		case "epub":
			dir := filepath.Join(chapRoot, "epub")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			out := filepath.Join(dir, base+".epub")
			author := strings.TrimSpace(defaultAuthor)
			if author == "" {
				author = "Unknown"
			}
			if err := writer.WriteEPUB([]model.Chapter{ch}, out, ch.Title, author, false); err != nil {
				return fmt.Errorf("epub %s: %w", ch.Title, err)
			}
			log.Printf("Wrote chapter EPUB: %s", out)
		}
	}
	return nil
}

func main() {
	// CLI flags
	startURL := flag.String("url", "", "Start URL to parse (required)")
	outDir := flag.String("out", "out", "Output directory")
	maxPages := flag.Int("max", 1, "Max pages to parse")
	delayMS := flag.Int("delay", 1000, "Polite delay between requests in ms")
	userAgent := flag.String("ua", "Go-Parser/1.0 (+https://github.com/vjovkovs/goparser)", "User-Agent header")

	// Book/output flags
	// bookFmt := flag.String("fmt", "none", "Final artifact format: epub|pdf|txt|none")
	// bookTitle := flag.String("title", "Untitled", "Book title for EPUB/PDF")
	// bookAuthor := flag.String("author", "Unknown", "Author for EPUB metadata")
	bookFmt := flag.String("fmt", "", "Final artifact format: epub|pdf|txt (omit or 'none' to skip)")
	bookTitle := flag.String("title", "", "Book title for EPUB/PDF; if empty, book is skipped")
	bookAuthor := flag.String("author", "", "Author for EPUB metadata (optional)")
	onlyChapters := flag.Bool("only-chapters", false, "Force skipping book artifacts, even if --fmt is set")
	// New: per-chapter formats (comma-separated: epub,pdf,txt)
	chapterFmt := flag.String("chapter-fmt", "", "Per-chapter formats to emit (comma-separated: epub,pdf,txt)")

	// Persist each chapter’s HTML/JSON as we go
	saveEach := flag.Bool("save-each", true, "Save each chapter asset while crawling")

	flag.Parse()

	if strings.TrimSpace(*startURL) == "" {
		log.Fatal("missing -url (Start URL)")
	}

	// Ensure output directory exists
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatalf("make out dir: %v", err)
	}

	// Context with Ctrl+C cancel
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// HTTP client
	client := fetch.NewClient(fetch.Options{
		Timeout:   15 * time.Second,
		UserAgent: *userAgent,
		Delay:     time.Duration(*delayMS) * time.Millisecond,
	})

	// Storage (for per-chapter saves)
	var store *storage.FS
	if *saveEach {
		store = storage.NewFS(*outDir)
	}

	// Orchestrate
	r := app.NewRunner(client, store, app.Options{
		MaxPages: *maxPages,
		SaveEach: *saveEach,
	})

	start := time.Now()
	res, err := r.Run(ctx, *startURL)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Parsed %d chapter(s) in %s", len(res.Chapters), time.Since(start).Round(time.Millisecond))

	chapterFormats := parseFormats(*chapterFmt)
	if len(chapterFormats) > 0 {
		for _, ch := range res.Chapters {
			if err := writePerChapterArtifacts(ch, *outDir, chapterFormats, *bookAuthor); err != nil {
				log.Fatalf("chapter export failed (%s): %v", ch.Title, err)
			}
		}
	}
	slugTitle := util.Slugify(*bookTitle)
	if slugTitle == "" {
		slugTitle = "book"
	}

	// Optional: compile final artifact
	wantFmt := strings.ToLower(strings.TrimSpace(*bookFmt))
	wantBook := !*onlyChapters && wantFmt != "" && wantFmt != "none" && strings.TrimSpace(*bookTitle) != ""

	if !wantBook {
		log.Printf("Skipping book artifact; per-chapter files saved under %s\\chapters\\...", *outDir)
		return
	}

	slugTitle = util.Slugify(*bookTitle)
	if slugTitle == "" {
		slugTitle = "book"
	}

	switch wantFmt {
	case "epub":
		dir := ensureSubdir(*outDir, "epub")
		out := filepath.Join(dir, slugTitle+".epub")
		// fall back author if omitted
		author := *bookAuthor
		if strings.TrimSpace(author) == "" {
			author = "Unknown"
		}
		if err := writer.WriteEPUB(res.Chapters, out, *bookTitle, author, true); err != nil {
			log.Fatal(err)
		}
		fmt.Println("EPUB:", out)
	case "pdf":
		dir := ensureSubdir(*outDir, "pdf")
		out := filepath.Join(dir, slugTitle+".pdf")
		if err := writer.WritePDF(res.Chapters, out); err != nil {
			log.Fatal(err)
		}
		fmt.Println("PDF:", out)
	case "txt":
		dir := ensureSubdir(*outDir, "txt")
		out := filepath.Join(dir, slugTitle+".txt")
		if err := writer.WriteTXT(res.Chapters, out); err != nil {
			log.Fatal(err)
		}
		fmt.Println("TXT:", out)
	default:
		log.Fatalf("unknown --fmt=%q (expected epub|pdf|txt)", *bookFmt)
	}
}
