package tts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Engine wraps espeak-ng CLI.
type Engine struct {
	ExePath  string // optional; if empty we'll auto-detect
	Voice    string // e.g., "en-us", "en", "en-GB"
	SpeedWPM int    // words per minute; espeak-ng -s (default ~175)
	MaxChars int    // max runes per chunk; ~1500-2500 is safe
}

// New returns a sensible default Engine.
func New() *Engine {
	return &Engine{
		Voice:    "en-us",
		SpeedWPM: 170,
		MaxChars: 1800,
	}
}

// SynthesizeFile reads a .txt file and generates sequential WAV chunks in outDir.
// baseName becomes the file prefix: <baseName>-part-0001.wav, etc.
// It also writes concat.txt (ffmpeg concat demuxer).
func (e *Engine) SynthesizeFile(ctx context.Context, txtPath, outDir, baseName string) ([]string, error) {
	b, err := os.ReadFile(txtPath)
	if err != nil {
		return nil, fmt.Errorf("read txt: %w", err)
	}
	return e.SynthesizeText(ctx, string(b), outDir, baseName)
}

// SynthesizeText splits and renders the text to WAV chunks in outDir.
func (e *Engine) SynthesizeText(ctx context.Context, text, outDir, baseName string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	exe := e.ExePath
	if exe == "" {
		exe = findEspeak()
		if exe == "" {
			return nil, fmt.Errorf("espeak-ng not found in PATH (ensure it’s installed)")
		}
	}

	chunks := splitSmart(text, max(400, e.MaxChars)) // floor at 400
	var files []string

	for i, chunk := range chunks {
		tmp, err := os.CreateTemp("", "tts-chunk-*.txt")
		if err != nil {
			return nil, err
		}
		tmpName := tmp.Name()
		if _, err := tmp.WriteString(chunk); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return nil, err
		}
		tmp.Close()
		out := filepath.Join(outDir, fmt.Sprintf("%s-part-%04d.wav", baseName, i+1))

		args := []string{
			"-w", out,
			"-v", e.Voice,
			"-s", fmt.Sprintf("%d", e.SpeedWPM),
			"-f", tmpName, // read text from temp file (avoids cmdline length issues)
		}
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			os.Remove(tmpName)
			return nil, fmt.Errorf("espeak-ng chunk %d: %w", i+1, err)
		}
		os.Remove(tmpName)
		files = append(files, out)
		// tiny politeness delay to keep CPU/fans sane on big runs
		time.Sleep(50 * time.Millisecond)
	}

	// Write concat.txt manifest (relative paths) for easy merge later.
	if err := writeConcatManifest(outDir, files); err != nil {
		return nil, err
	}
	return files, nil
}

// ----------------- helpers -----------------

func findEspeak() string {
	candidates := []string{"espeak-ng"}
	if runtime.GOOS == "windows" {
		candidates = append([]string{"espeak-ng.exe"}, candidates...)
	}
	for _, c := range candidates {
		if p, _ := exec.LookPath(c); p != "" {
			return p
		}
	}
	return ""
}

// splitSmart breaks text into chunks up to maxChars, preferring sentence boundaries.
// func splitSmart(text string, maxChars int) []string {
// 	// normalize newlines and collapse long whitespace runs
// 	s := strings.ReplaceAll(text, "\r\n", "\n")
// 	ws := regexp.MustCompile(`[ \t\f\v]+`)
// 	s = ws.ReplaceAllString(s, " ")

// 	// rough sentence tokens (., !, ?, ;, and double newlines as hard break)
// 	reSent := regexp.MustCompile(`(?s).+?(?:[\.!\?]+["')\]]*\s+|\n{2,}|$)`)
// 	sents := reSent.FindAllString(s, -1)

// 	var chunks []string
// 	var cur strings.Builder

// 	for _, sen := range sents {
// 		if cur.Len()+len(sen) > maxChars && cur.Len() > 0 {
// 			chunks = append(chunks, strings.TrimSpace(cur.String()))
// 			cur.Reset()
// 		}
// 		if len(sen) > maxChars {
// 			// sentence itself is huge; hard-wrap at whitespace
// 			sub := hardWrap(sen, maxChars)
// 			for _, part := range sub {
// 				if cur.Len()+len(part) > maxChars && cur.Len() > 0 {
// 					chunks = append(chunks, strings.TrimSpace(cur.String()))
// 					cur.Reset()
// 				}
// 				cur.WriteString(part)
// 			}
// 			continue
// 		}
// 		cur.WriteString(sen)
// 	}
// 	if strings.TrimSpace(cur.String()) != "" {
// 		chunks = append(chunks, strings.TrimSpace(cur.String()))
// 	}
// 	return chunks
// }

// func hardWrap(s string, maxChars int) []string {
// 	var parts []string
// 	for len(s) > maxChars {
// 		cut := lastSpaceBefore(s, maxChars)
// 		parts = append(parts, strings.TrimSpace(s[:cut])+"\n")
// 		s = strings.TrimSpace(s[cut:])
// 	}
// 	if s != "" {
// 		parts = append(parts, s)
// 	}
// 	return parts
// }

// func lastSpaceBefore(s string, idx int) int {
// 	if idx >= len(s) {
// 		return len(s)
// 	}
// 	for i := idx; i > 0; i-- {
// 		if s[i] == ' ' || s[i] == '\n' || s[i] == '\t' {
// 			return i
// 		}
// 	}
// 	return idx
// }

// func writeConcatManifest(outDir string, files []string) error {
// 	// sort just in case
// 	sort.Strings(files)
// 	fp := filepath.Join(outDir, "concat.txt")
// 	f, err := os.Create(fp)
// 	if err != nil {
// 		return err
// 	}
// 	defer f.Close()

// 	w := bufio.NewWriter(f)
// 	for _, p := range files {
// 		rel, _ := filepath.Rel(outDir, p)
// 		// ffmpeg concat demuxer expects: file '<path>'
// 		_, _ = fmt.Fprintf(w, "file '%s'\n", filepath.ToSlash(rel))
// 	}
// 	return w.Flush()
// }

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
