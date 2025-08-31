package util

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Slugify turns "My Book: Äß • Vol. 1" -> "my-book-ass-vol-1"
func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Decompose accents (NFD) and drop combining marks.
	decomposed := norm.NFD.String(s)

	var b strings.Builder
	b.Grow(len(decomposed))

	prevHyphen := false
	for _, r := range decomposed {
		// Strip diacritics
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			// Any other char becomes a single hyphen (collapse runs)
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 64 {
		slug = slug[:64]
	}
	return slug
}
