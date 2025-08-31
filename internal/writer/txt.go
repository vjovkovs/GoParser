package writer

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vjovkovs/goparser/internal/model"
	"golang.org/x/net/html"
)

func stripHTMLPreserveFormatting(h string) string {
	tok := html.NewTokenizer(strings.NewReader(h))
	var out []string
	var lastBlock bool

	emitNL := func() { out = append(out, "\n") }

	for {
		tt := tok.Next()
		switch tt {
		case html.ErrorToken:
			return strings.TrimRight(strings.Join(out, ""), "\n")
		case html.TextToken:
			out = append(out, string(tok.Text()))
		case html.StartTagToken, html.SelfClosingTagToken:
			t, _ := tok.TagName()
			tag := strings.ToLower(string(t))
			switch tag {
			case "br", "hr":
				emitNL()
			case "p", "div", "blockquote", "li":
				if !lastBlock {
					emitNL()
				}
				lastBlock = true
			default:
				lastBlock = false
			}
		case html.EndTagToken:
			t, _ := tok.TagName()
			tag := strings.ToLower(string(t))
			switch tag {
			case "p", "div", "blockquote", "li":
				emitNL()
				lastBlock = false
			}
		}
	}
}

func WriteTXT(chs []model.Chapter, outPath string) error {
	if len(chs) == 0 {
		return fmt.Errorf("WriteTXT: no chapters provided")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for i, ch := range chs {
		if i > 0 {
			if _, err := w.WriteString("\n"); err != nil {
				return err
			}
		}
		if _, err := w.WriteString(ch.Title + "\n"); err != nil {
			return err
		}
		text := stripHTMLPreserveFormatting(ch.HTML)
		if _, err := w.WriteString(strings.TrimRight(text, "\n") + "\n"); err != nil {
			return err
		}
	}
	return w.Flush()
}
