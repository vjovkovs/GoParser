package tts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// CombineWAV concatenates multiple PCM WAV files into one WAV.
// All inputs must share the same sample rate and channel count.
func CombineWAV(inputs []string, outPath string) error {
	if len(inputs) == 0 {
		return fmt.Errorf("CombineWAV: no inputs")
	}
	// stable order just in case
	sort.Strings(inputs)

	// Open first file to capture format + bit depth
	first, err := os.Open(inputs[0])
	if err != nil {
		return err
	}
	defer first.Close()

	dec0 := wav.NewDecoder(first)
	if !dec0.IsValidFile() {
		return fmt.Errorf("invalid WAV: %s", inputs[0])
	}
	buf0, err := dec0.FullPCMBuffer()
	if err != nil {
		return fmt.Errorf("read %s: %w", inputs[0], err)
	}
	fmt0 := buf0.Format
	bitDepth := buf0.SourceBitDepth
	if bitDepth == 0 {
		// espeak-ng typically outputs 16-bit PCM; default to 16
		bitDepth = 16
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()

	enc := wav.NewEncoder(out, fmt0.SampleRate, bitDepth, fmt0.NumChannels, 1)
	defer enc.Close()

	// write first buffer
	if err := enc.Write(buf0); err != nil {
		return fmt.Errorf("write %s: %w", inputs[0], err)
	}

	// append the rest
	for i := 1; i < len(inputs); i++ {
		f, err := os.Open(inputs[i])
		if err != nil {
			return err
		}
		dec := wav.NewDecoder(f)
		if !dec.IsValidFile() {
			f.Close()
			return fmt.Errorf("invalid WAV: %s", inputs[i])
		}
		buf, err := dec.FullPCMBuffer()
		f.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", inputs[i], err)
		}
		if !sameFormat(fmt0, buf.Format) {
			return fmt.Errorf("format mismatch in %s (expected %d Hz, %d ch; got %d Hz, %d ch)",
				inputs[i], fmt0.SampleRate, fmt0.NumChannels, buf.Format.SampleRate, buf.Format.NumChannels)
		}
		// We keep the output bit depth as the first buffer's bit depth.
		if err := enc.Write(buf); err != nil {
			return fmt.Errorf("write %s: %w", inputs[i], err)
		}
	}
	return nil
}

func sameFormat(a, b *audio.Format) bool {
	return a != nil && b != nil &&
		a.SampleRate == b.SampleRate &&
		a.NumChannels == b.NumChannels
}
