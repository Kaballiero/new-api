package openrouter

import (
	"bytes"
	"context"
	"math"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// transcriptionSeconds is the duration of a transcription file in whole
// seconds, rounded up, read from its audio headers (owner rule of 14.11): by
// the file extension (.mpga and .mpeg are MPEG audio), then, when that fails,
// by the format its magic bytes name. The headers are client-controlled, so
// the duration is capped by what the file size holds at 8 kbit/s, 1000 bytes
// a second, before it is converted; the file is at most 25 MiB, so the
// seconds fit int64. readable is false when no reader yields a positive
// finite duration.
func transcriptionSeconds(ctx context.Context, filename string, audio []byte) (seconds int64, readable bool) {
	extension := strings.ToLower(filepath.Ext(filename))
	switch extension {
	case ".mpga", ".mpeg":
		extension = ".mp3"
	}
	var sniffed string
	switch {
	case len(audio) >= 12 && string(audio[:4]) == "RIFF" && string(audio[8:12]) == "WAVE":
		sniffed = ".wav"
	case bytes.HasPrefix(audio, []byte("ID3")), len(audio) >= 2 && audio[0] == 0xFF && audio[1]&0xE0 == 0xE0 && audio[1]&0x06 != 0:
		// An MPEG audio frame sync with a layer; layer 0 is AAC ADTS.
		sniffed = ".mp3"
	case bytes.HasPrefix(audio, []byte("OggS")):
		sniffed = ".ogg"
	case bytes.HasPrefix(audio, []byte("fLaC")):
		sniffed = ".flac"
	case len(audio) >= 8 && string(audio[4:8]) == "ftyp":
		sniffed = ".m4a"
	case bytes.HasPrefix(audio, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		sniffed = ".webm"
	}
	formats := []string{extension}
	if sniffed != "" && sniffed != extension {
		formats = append(formats, sniffed)
	}
	for _, format := range formats {
		duration, err := common.GetAudioDuration(ctx, bytes.NewReader(audio), format)
		// NaN fails duration > 0.
		if err == nil && duration > 0 && !math.IsInf(duration, 1) {
			return int64(math.Ceil(min(duration, float64(len(audio))/1000))), true
		}
	}
	return 0, false
}
