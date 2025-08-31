package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vjovkovs/goparser/internal/model"
)

type FS struct{ dir string }

func NewFS(dir string) *FS {
	_ = os.MkdirAll(dir, 0o755)
	return &FS{dir: dir}
}

func (f *FS) Save(ch model.Chapter) error {
	base := safeName(ch.Code)
	if base == "" {
		base = safeName(ch.Title)
	}
	if base == "" {
		base = "chapter"
	}

	// New: per-chapter subdirectory like out/chapters/<slug>/
	chapDir := filepath.Join(f.dir, "chapters", base)
	if err := os.MkdirAll(chapDir, 0o755); err != nil {
		return err
	}

	// Write files inside that folder
	htmlPath := filepath.Join(chapDir, "index.html")
	metaPath := filepath.Join(chapDir, "meta.json")

	if err := os.WriteFile(htmlPath, []byte(ch.HTML), 0o644); err != nil {
		return err
	}

	// Point metadata to the relative HTML path
	meta := map[string]string{
		"title": ch.Title,
		"code":  ch.Code,
		"url":   ch.URL,
		"html":  filepath.ToSlash(filepath.Join("chapters", base, "index.html")),
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(metaPath, b, 0o644)
}

// func (f *FS) Save(ch model.Chapter) error {
// 	base := safeName(ch.Code)
// 	if base == "" { base = safeName(ch.Title) }
// 	if base == "" { base = "chapter" }

// 	htmlPath := filepath.Join(f.dir, base + ".html")
// 	metaPath := filepath.Join(f.dir, base + ".json")

// 	if err := os.WriteFile(htmlPath, []byte(ch.HTML), 0o644); err != nil { return err }
// 	meta := map[string]string{"title": ch.Title, "code": ch.Code, "url": ch.URL, "html": filepath.Base(htmlPath)}
// 	b, _ := json.MarshalIndent(meta, "", "  ")
// 	return os.WriteFile(metaPath, b, 0o644)
// }

var invalid = regexp.MustCompile(`[^a-z0-9\-]+`)

func safeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "â€”", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = invalid.ReplaceAllString(s, "")
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}
