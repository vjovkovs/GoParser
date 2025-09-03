package tts

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	ttsv1 "github.com/vjovkovs/goparser/api/tts/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Options for Kokoro.
type KokoroOptions struct {
	Addr        string  // "host:port"
	Insecure    bool    // true for plaintext (dev); false for TLS in prod
	Voice       string  // "en-us"
	SampleRate  int     // e.g., 24000
	AudioFormat string  // "wav" (recommended for chunking/combining)
	Speed       float32 // 1.0 = normal
	MaxChars    int     // chunk size safeguard (~1500-2500 ideal)
}

// KokoroEngine streams audio via gRPC.
type KokoroEngine struct {
	opt    KokoroOptions
	conn   *grpc.ClientConn
	client ttsv1.TTSClient
}

// NewKokoroEngine dials the Kokoro server and returns a ready client.
func NewKokoroEngine(ctx context.Context, opt KokoroOptions) (*KokoroEngine, error) {
	if strings.TrimSpace(opt.Addr) == "" {
		return nil, fmt.Errorf("kokoro addr is required")
	}
	dialOpts := []grpc.DialOption{}
	if opt.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		// TODO: add TLS creds here for production
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.DialContext(ctx, opt.Addr, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("dial kokoro: %w", err)
	}
	return &KokoroEngine{
		opt:    opt,
		conn:   conn,
		client: ttsv1.NewTTSClient(conn),
	}, nil
}

func (e *KokoroEngine) Close() error {
	if e.conn != nil {
		return e.conn.Close()
	}
	return nil
}

// SynthesizeFile reads text from a file then calls SynthesizeText.
func (e *KokoroEngine) SynthesizeFile(ctx context.Context, txtPath, outDir, baseName string) ([]string, error) {
	b, err := os.ReadFile(txtPath)
	if err != nil {
		return nil, fmt.Errorf("read txt: %w", err)
	}
	return e.SynthesizeText(ctx, string(b), outDir, baseName)
}

// SynthesizeText splits huge text and requests one Kokoro stream per chunk.
// Each chunk is written as baseName-part-0001.wav (or .mp3/.pcm depending on AudioFormat).
func (e *KokoroEngine) SynthesizeText(ctx context.Context, text, outDir, baseName string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	maxc := e.opt.MaxChars
	if maxc <= 0 {
		maxc = 1800
	}
	chunks := splitSmart(text, maxc)

	ext := strings.ToLower(e.opt.AudioFormat)
	if ext == "" {
		ext = "wav"
	}
	// Normalize extension start
	switch ext {
	case "wav", "mp3", "pcm_s16le":
	default:
		ext = "wav"
	}

	var files []string
	for i, chunk := range chunks {
		filename := fmt.Sprintf("%s-part-%04d.%s", baseName, i+1, ext)
		outPath := filepath.Join(outDir, filename)

		if err := e.synthesizeOne(ctx, chunk, outPath); err != nil {
			return nil, fmt.Errorf("chunk %d: %w", i+1, err)
		}
		files = append(files, outPath)
		// tiny delay to be gentle on the server
		time.Sleep(30 * time.Millisecond)
	}

	// Keep concat.txt around for reference (ffmpeg concat style), even though we have a Go combiner.
	if err := writeConcatManifest(outDir, files); err != nil {
		return nil, err
	}
	return files, nil
}

// synthesizeOne requests a single TTS stream and writes it directly to a file.
func (e *KokoroEngine) synthesizeOne(ctx context.Context, text, outPath string) error {
	req := &ttsv1.SynthesizeRequest{
		Text:         text,
		Voice:        e.opt.Voice,
		SampleRateHz: int32(e.opt.SampleRate),
		AudioFormat:  e.opt.AudioFormat, // recommend "wav"
		Speed:        e.opt.Speed,
	}
	stream, err := e.client.Synthesize(ctx, req)
	if err != nil {
		return fmt.Errorf("kokoro synth: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return fmt.Errorf("stream recv: %w", recvErr)
		}
		if _, werr := f.Write(chunk.GetAudio()); werr != nil {
			return werr
		}
	}
	return nil
}

// ----------------- text split helpers (same as before) -----------------

func splitSmart(text string, maxChars int) []string {
	s := strings.ReplaceAll(text, "\r\n", "\n")
	ws := regexp.MustCompile(`[ \t\f\v]+`)
	s = ws.ReplaceAllString(s, " ")

	reSent := regexp.MustCompile(`(?s).+?(?:[\.!\?]+["')\]]*\s+|\n{2,}|$)`)
	sents := reSent.FindAllString(s, -1)

	var chunks []string
	var cur strings.Builder

	for _, sen := range sents {
		if cur.Len()+len(sen) > maxChars && cur.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		if len(sen) > maxChars {
			for _, part := range hardWrap(sen, maxChars) {
				if cur.Len()+len(part) > maxChars && cur.Len() > 0 {
					chunks = append(chunks, strings.TrimSpace(cur.String()))
					cur.Reset()
				}
				cur.WriteString(part)
			}
			continue
		}
		cur.WriteString(sen)
	}
	if strings.TrimSpace(cur.String()) != "" {
		chunks = append(chunks, strings.TrimSpace(cur.String()))
	}
	return chunks
}

func hardWrap(s string, maxChars int) []string {
	var parts []string
	for len(s) > maxChars {
		cut := lastSpaceBefore(s, maxChars)
		parts = append(parts, strings.TrimSpace(s[:cut])+"\n")
		s = strings.TrimSpace(s[cut:])
	}
	if s != "" {
		parts = append(parts, s)
	}
	return parts
}

func lastSpaceBefore(s string, idx int) int {
	if idx >= len(s) {
		return len(s)
	}
	for i := idx; i > 0; i-- {
		if s[i] == ' ' || s[i] == '\n' || s[i] == '\t' {
			return i
		}
	}
	return idx
}

// manifest writer reused by CLI (for reference / external tools)
func writeConcatManifest(outDir string, files []string) error {
	sort.Strings(files)
	fp := filepath.Join(outDir, "concat.txt")
	f, err := os.Create(fp)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, p := range files {
		rel, _ := filepath.Rel(outDir, p)
		_, _ = fmt.Fprintf(f, "file '%s'\n", filepath.ToSlash(rel))
	}
	return nil
}
