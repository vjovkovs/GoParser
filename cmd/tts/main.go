package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	ttsv1 "github.com/vjovkovs/goparser/api/tts/v1"
	"github.com/vjovkovs/goparser/internal/tts"
	"github.com/vjovkovs/goparser/internal/util"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	in := flag.String("in", "", "Path to input .txt OR directory of .txt files (required)")
	out := flag.String("out", "out/audio", "Output directory root")
	base := flag.String("base", "", "Base filename when -in is a single file (default from filename)")

	// Kokoro connection
	addr := flag.String("kokoro-addr", "localhost:50051", "Kokoro gRPC address (host:port)")
	insecure := flag.Bool("kokoro-insecure", true, "Use plaintext connection (dev)")

	// Synthesis params
	// voice := flag.String("voice", "en-us", "Voice id")
	voice := flag.String("voice", "af_heart", "Kokoro voice id (e.g. af_heart, bf_emma). Aliases: en-us->af_heart, en-gb->bf_emma")
	speed := flag.Float64("speed", 1.0, "Speaking rate (1.0 = normal)")
	rate := flag.Int("rate", 24000, "Sample rate (Hz)")
	maxc := flag.Int("max", 1800, "Max chars per chunk")
	format := flag.String("format", "wav", "Requested audio format from server (wav|mp3|pcm_s16le) — recommend wav")

	// Batch options
	recursive := flag.Bool("recursive", false, "When -in is a directory, include subdirectories")
	final := flag.String("final", "mp3", "Final combined output: mp3|wav|none")
	listVoices := flag.Bool("list-voices", false, "Query server for available voices and exit")

	flag.Parse()
	if *listVoices {
		if err := doListVoices(*addr, *insecure); err != nil {
			log.Fatal(err)
		}
		return
	}

	if strings.TrimSpace(*in) == "" {
		log.Fatal("missing -in (path to .txt or directory). Omit -in only when using --list-voices.")
	}

	inPath := abs(expand(*in))
	outRoot := abs(expand(*out))

	// Collect inputs
	files, err := collectInputs(inPath, *recursive)
	if err != nil {
		log.Fatal(err)
	}
	if len(files) == 0 {
		log.Fatalf("no .txt found at %s", inPath)
	}

	// Build Kokoro engine
	ctx, cancel := context.WithTimeout(context.Background(), 48*time.Hour)
	defer cancel()

	engine, err := tts.NewKokoroEngine(ctx, tts.KokoroOptions{
		Addr:        *addr,
		Insecure:    *insecure,
		Voice:       *voice,
		SampleRate:  *rate,
		AudioFormat: *format,
		Speed:       float32(*speed),
		MaxChars:    *maxc,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Close()

	for _, f := range files {
		var baseName string
		if len(files) == 1 && strings.TrimSpace(*base) != "" {
			baseName = util.Slugify(*base)
		} else {
			baseName = util.Slugify(strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
		}
		if baseName == "" {
			baseName = "audiofile"
		}

		// Per-item output root: out/audio/<baseName>/
		itemRoot := filepath.Join(outRoot, baseName)
		partsDir := filepath.Join(itemRoot, "parts")
		if err := os.MkdirAll(partsDir, 0o755); err != nil {
			log.Fatal(err)
		}

		log.Printf("Synthesizing %s -> %s", f, partsDir)
		wavsOrFiles, err := engine.SynthesizeFile(ctx, f, partsDir, baseName)
		if err != nil {
			log.Fatalf("synthesize %s: %v", f, err)
		}

		// If server returned non-wav files (e.g., mp3), we skip WAV combine and only do final MP3 join (with external tool).
		allExt := extSet(wavsOrFiles)
		usingWAV := len(allExt) == 1 && strings.EqualFold(firstKey(allExt), ".wav")

		// Combine
		switch strings.ToLower(*final) {
		case "none":
			log.Printf("Chunks written; skipping final combine: %s", partsDir)
		case "wav":
			if usingWAV {
				finalWAV := filepath.Join(itemRoot, baseName+"-full.wav")
				if err := tts.CombineWAV(wavsOrFiles, finalWAV); err != nil {
					log.Fatalf("combine wav: %v", err)
				}
				log.Printf("WAV: %s", finalWAV)
			} else {
				log.Printf("Server format is not WAV (%v); cannot WAV-combine. Use -format=wav or -final=mp3.", keys(allExt))
			}
		case "mp3":
			if usingWAV {
				finalWAV := filepath.Join(itemRoot, baseName+"-full.wav")
				if err := tts.CombineWAV(wavsOrFiles, finalWAV); err != nil {
					log.Fatalf("combine wav: %v", err)
				}
				mp3Path := filepath.Join(itemRoot, baseName+"-full.mp3")
				if err := encodeMP3(finalWAV, mp3Path); err != nil {
					log.Printf("mp3 encode failed: %v (keeping WAV)", err)
				} else {
					log.Printf("MP3: %s", mp3Path)
					// optional: _ = os.Remove(finalWAV)
				}
			} else {
				// Fallback: try to concat MP3s via ffmpeg (requires re-encode unless identical CBR).
				mp3Path := filepath.Join(itemRoot, baseName+"-full.mp3")
				if err := concatMP3(wavsOrFiles, mp3Path); err != nil {
					log.Printf("mp3 concat failed; consider -format=wav then combine: %v", err)
				} else {
					log.Printf("MP3: %s", mp3Path)
				}
			}
		default:
			log.Fatalf("unknown -final=%q (mp3|wav|none)", *final)
		}
	}
	log.Println("Done.")
}

// ---------- helpers ----------

func doListVoices(addr string, insecureConn bool) error {
	var opts []grpc.DialOption
	if insecureConn {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		// TODO: add TLS creds here
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	c := ttsv1.NewTTSClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.ListVoices(ctx, &ttsv1.ListVoicesRequest{})
	if err != nil {
		return fmt.Errorf("ListVoices: %w", err)
	}

	fmt.Println("Voices:")
	for _, v := range resp.GetVoices() {
		fmt.Printf("  %-12s  lang=%s  gender=%-6s  %s\n", v.GetId(), v.GetLangCode(), v.GetGender(), v.GetDisplay())
	}
	if len(resp.GetAliases()) > 0 {
		fmt.Println("\nAliases:")
		for _, a := range resp.GetAliases() {
			fmt.Printf("  %-10s -> %s\n", a.GetAlias(), a.GetMapsTo())
		}
	}
	return nil
}

func collectInputs(path string, recursive bool) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		if strings.EqualFold(filepath.Ext(path), ".txt") {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("not a .txt: %s", path)
	}
	var out []string
	if recursive {
		err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(p), ".txt") {
				out = append(out, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		ents, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(path, e.Name())
			if strings.EqualFold(filepath.Ext(p), ".txt") {
				out = append(out, p)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func encodeMP3(wavPath, mp3Path string) error {
	if exe := which("lame"); exe != "" {
		cmd := exec.Command(exe, "--silent", "-V2", wavPath, mp3Path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	if exe := which("ffmpeg"); exe != "" {
		cmd := exec.Command(exe, "-y", "-i", wavPath, "-vn", "-ar", "44100", "-ac", "2", "-b:a", "192k", mp3Path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("no MP3 encoder found (install 'lame' or 'ffmpeg')")
}

// BEST-EFFORT MP3 concat: will re-mux/re-encode with ffmpeg. Prefer WAV combine if possible.
func concatMP3(parts []string, outMP3 string) error {
	if len(parts) == 0 {
		return fmt.Errorf("no inputs")
	}
	if exe := which("ffmpeg"); exe != "" {
		// Build a temporary list file
		dir := filepath.Dir(outMP3)
		lf := filepath.Join(dir, "mp3list.txt")
		f, err := os.Create(lf)
		if err != nil {
			return err
		}
		for _, p := range parts {
			rel, _ := filepath.Rel(dir, p)
			_, _ = fmt.Fprintf(f, "file '%s'\n", filepath.ToSlash(rel))
		}
		_ = f.Close()
		cmd := exec.Command(exe, "-y", "-f", "concat", "-safe", "0", "-i", lf, "-c:a", "libmp3lame", "-q:a", "2", outMP3)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		_ = os.Remove(lf)
		return err
	}
	return fmt.Errorf("ffmpeg not found for MP3 concat; prefer -format=wav then -final=mp3")
}

func which(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		if p, _ := exec.LookPath(name + ".exe"); p != "" {
			return p
		}
	}
	if p, _ := exec.LookPath(name); p != "" {
		return p
	}
	return ""
}

func expand(p string) string {
	if p == "" {
		return p
	}
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
func abs(p string) string {
	if ap, err := filepath.Abs(p); err == nil {
		return ap
	}
	return p
}

func extSet(paths []string) map[string]bool {
	m := map[string]bool{}
	for _, p := range paths {
		m[strings.ToLower(filepath.Ext(p))] = true
	}
	return m
}
func firstKey(m map[string]bool) string {
	for k := range m {
		return k
	}
	return ""
}
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
