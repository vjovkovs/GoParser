package writer

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmaupin/go-epub"
	"github.com/google/uuid"

	"github.com/vjovkovs/goparser/internal/model"
)

const epubCSS = `
html, body { margin:0; padding:0; }
body { line-height:1.45; }
h1, h2, h3 { margin:0 0 0.8em 0; }
p { margin:0 0 1em 0; }
blockquote { margin:0.6em 1.2em; }
ul, ol { margin:0 0 1em 1.2em; padding:0; }
hr.scene-break { border:none; border-top:1px solid #888; margin:1.2em 0; width:40%; }
p.spacer { margin:0 0 1.6em 0; }
a { text-decoration:none; } a:link, a:visited { color:inherit; }
`

func safeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	r := strings.NewReplacer("—", "-", " ", "-", "/", "-", ":", "", "|", "", "?", "", "*", "", "\"", "", "<", "", ">", "")
	return r.Replace(s)
}

func WriteEPUB(chs []model.Chapter, outPath, title, author string, includeTOC bool) error {
	if len(chs) == 0 {
		return fmt.Errorf("WriteEPUB: no chapters provided")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	book := epub.NewEpub(title)
	book.SetAuthor(author)
	book.SetLang("en")
	book.SetIdentifier("urn:uuid:" + uuid.NewString())

	// Drop CSS to a temp file so we can reference it
	tmpCSS := filepath.Join(os.TempDir(), "goparser-style.css")
	if err := os.WriteFile(tmpCSS, []byte(epubCSS), 0o644); err != nil {
		return err
	}
	cssRef, err := book.AddCSS(tmpCSS, "style.css") // NB: go-epub exposes AddCSS; if your version differs, pass just the path
	if err != nil {
		return err
	}

	// Optional human-readable ToC page
	if includeTOC {
		var items []string
		for i, ch := range chs {
			f := fmt.Sprintf("ch_%03d_%s.xhtml", i+1, safeName(ch.Code))
			items = append(items, fmt.Sprintf(`<li><a href="%s">%s</a></li>`, f, html.EscapeString(ch.Title)))
		}
		tocXHTML := fmt.Sprintf(
			`<h1>Table of Contents</h1><ol>%s</ol>`,
			strings.Join(items, ""),
		)
		// filename set so it appears before chapters in spine
		book.AddSection(tocXHTML, "Table of Contents", "toc.xhtml", cssRef)
	}

	// Chapters
	for i, ch := range chs {
		fn := fmt.Sprintf("ch_%03d_%s.xhtml", i+1, safeName(ch.Code))
		body := fmt.Sprintf(`<h2>%s</h2>%s`, html.EscapeString(ch.Title), ch.HTML)
		book.AddSection(body, ch.Title, fn, cssRef)
	}

	return book.Write(outPath)
}
