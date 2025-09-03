// internal/storage/fs.go
package storage

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/vjovkovs/goparser/internal/model"
	"github.com/vjovkovs/goparser/internal/util"
)

type FS struct{ dir string }

func NewFS(dir string) *FS {
	_ = os.MkdirAll(dir, 0o755)
	return &FS{dir: dir}
}

func (f *FS) Save(ch model.Chapter) error {
	slug := util.ChapterSlug(ch)

	// out/chapters/<slug>/
	chapDir := filepath.Join(f.dir, "chapters", slug)
	if err := os.MkdirAll(chapDir, 0o755); err != nil {
		return err
	}

	htmlPath := filepath.Join(chapDir, "index.html")
	metaPath := filepath.Join(chapDir, "meta.json")

	if err := os.WriteFile(htmlPath, []byte(ch.HTML), 0o644); err != nil {
		return err
	}

	meta := map[string]string{
		"title": ch.Title,
		"code":  ch.Code,
		"url":   ch.URL,
		"html":  filepath.ToSlash(filepath.Join("chapters", slug, "index.html")),
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(metaPath, b, 0o644)
}
