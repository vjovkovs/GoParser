package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	ttsv1 "github.com/vjovkovs/goparser/api/tts/v1"
	"google.golang.org/grpc"
)

// --- server ---

type server struct {
	ttsv1.UnimplementedTTSServer
	voice string
	speed int
}

func (s *server) Synthesize(req *ttsv1.SynthesizeRequest, stream ttsv1.TTS_SynthesizeServer) error {
	// For now: synthesize with espeak-ng to a temp WAV, then stream chunks.
	// Later: replace this with your Kokoro engine call.
	out, cleanup, err := synthWithEspeak(req.GetText(), pick(req.GetVoice(), s.voice), req.GetSpeed(), s.speed)
	if err != nil {
		return err
	}
	defer cleanup()

	f, err := os.Open(out)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if err := stream.Send(&ttsv1.AudioChunk{Audio: buf[:n]}); err != nil {
				return err
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

func synthWithEspeak(text, voice string, speedReq float32, fallbackSpeed int) (string, func(), error) {
	tmpDir := os.TempDir()
	wav := filepath.Join(tmpDir, fmt.Sprintf("tts-%d.wav", time.Now().UnixNano()))
	tmpTxt := filepath.Join(tmpDir, fmt.Sprintf("tts-%d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(tmpTxt, []byte(text), 0o644); err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.Remove(wav); _ = os.Remove(tmpTxt) }

	speed := fallbackSpeed
	if speedReq > 0 {
		// espeak-ng speed is WPM; map roughly from 1.0=normal
		speed = int(170 * speedReq)
	}

	// Requires espeak-ng in PATH (works on Windows too: espeak-ng.exe)
	cmd := exec.Command("espeak-ng", "-w", wav, "-v", voice, "-s", fmt.Sprintf("%d", speed), "-f", tmpTxt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", cleanup, fmt.Errorf("espeak-ng: %w", err)
	}
	return wav, cleanup, nil
}

func pick(a string, fallback string) string {
	if a != "" {
		return a
	}
	return fallback
}

// --- main ---

func main() {
	listen := flag.String("listen", "localhost:50051", "gRPC listen address (host:port)")
	voice := flag.String("voice", "en-us", "Default voice")
	speed := flag.Int("speed", 170, "Default WPM if request does not provide speed")
	flag.Parse()

	lis, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	ttsv1.RegisterTTSServer(grpcServer, &server{voice: *voice, speed: *speed})

	log.Printf("Kokoro-compatible TTS gRPC server listening at %s", *listen)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
	_ = context.Background()
}
