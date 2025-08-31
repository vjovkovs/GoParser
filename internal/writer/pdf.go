package writer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jung-kurt/gofpdf"
	"github.com/vjovkovs/goparser/internal/model"
)

func registerRoboto(pdf *gofpdf.Fpdf, fontDir string) error {
	fonts := []struct{ name, style, file string }{
		{"Roboto", "", "Roboto-Regular.ttf"},
		{"Roboto", "B", "Roboto-Bold.ttf"},
		{"Roboto", "I", "Roboto-Italic.ttf"},
		{"Roboto", "BI", "Roboto-BoldItalic.ttf"},
	}
	for _, f := range fonts {
		fontPath := filepath.Join(fontDir, f.file)
		if _, err := os.Stat(fontPath); err != nil {
			return fmt.Errorf("font not found: %s: %w", fontPath, err)
		}
		pdf.AddUTF8Font(f.name, f.style, fontPath)
	}
	return nil
}

func htmlForFPDFBasic(s string) string {
	repl := []string{
		"<strong>", "<b>", "</strong>", "</b>",
		"<em>", "<i>", "</em>", "</i>",
		"<blockquote>", "<br><br>", "</blockquote>", "<br><br>",
		"<hr class=\"scene-break\"/>", "<br><br>",
		"<hr/>", "<br><br>",
		"</p>", "<br>",
		"<p>", "",
	}
	return strings.NewReplacer(repl...).Replace(s)
}

func WritePDF(chs []model.Chapter, outPath string) error {
	if len(chs) == 0 {
		return fmt.Errorf("WritePDF: no chapters provided")
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	pdf := gofpdf.New("P", "mm", "Letter", "")
	pdf.SetAutoPageBreak(true, 15)
	pdf.SetMargins(18, 18, 18)

	// Register Roboto from the repo
	if err := registerRoboto(pdf, "assets/fonts"); err != nil {
		return fmt.Errorf("register Roboto: %w", err)
	}

	for _, ch := range chs {
		pdf.AddPage()

		pdf.SetFont("Roboto", "B", 16)
		pdf.MultiCell(0, 8, ch.Title, "", "L", false)
		pdf.Ln(3)

		pdf.SetFont("Roboto", "", 12)
		html := pdf.HTMLBasicNew()
		body := htmlForFPDFBasic(ch.HTML)
		html.Write(5.5, body)
	}

	return pdf.OutputFileAndClose(outPath)
}
