package util

import (
	"strings"
	"unicode"

	"github.com/vjovkovs/goparser/internal/model"
	"golang.org/x/text/unicode/norm"
)

// Slugify turns arbitrary text into a filesystem-friendly slug.
// e.g., "Chapter 1.01 — Arrival?" -> "chapter-1-01-arrival"
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	decomposed := norm.NFD.String(s)

	var b strings.Builder
	b.Grow(len(decomposed))

	prevHyphen := false
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) { // drop combining marks
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 64 { // unified max length
		slug = slug[:64]
	}
	return slug
}

// ChapterSlug prefers Code, then Title, with a safe fallback.
func ChapterSlug(ch model.Chapter) string {
	if s := Slugify(strings.TrimSpace(ch.Code)); s != "" {
		return s
	}
	if s := Slugify(ch.Title); s != "" {
		return s
	}
	return "chapter"
}
