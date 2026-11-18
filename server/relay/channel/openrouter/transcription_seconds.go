package openrouter

import (
	"bytes"
	"context"
	"io"
	"math"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/go-audio/wav"
)

// transcriptionMaxSeconds bounds the seconds of a transcription file; client
// headers claiming more saturate to it.
const transcriptionMaxSeconds = 24 * 60 * 60

// transcriptionSeconds is the duration of a transcription file in whole
// seconds, rounded up, read from its audio headers (owner rule of 14.11). The
// client names the file and writes its headers, so neither may lower the
// reserve: the duration is the longest any reader yields, the reader of the
// file extension (.mpga and .mpeg are MPEG audio) and the reader of the
// format its magic bytes name, and for WAV also the duration of the bytes
// after the data chunk header, whatever length that header claims. It
// saturates to transcriptionMaxSeconds before it is converted. readable is
// false when no reader yields a positive finite duration.
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
	duration := 0.0
	for _, format := range formats {
		read, err := common.GetAudioDuration(ctx, bytes.NewReader(audio), format)
		// NaN fails read > 0.
		if err == nil && read > 0 && !math.IsInf(read, 1) {
			duration = max(duration, read)
		}
	}
	if sniffed == ".wav" {
		reader := bytes.NewReader(audio)
		decoder := wav.NewDecoder(reader)
		if decoder.FwdToPCM() == nil && decoder.PCMChunk != nil {
			dataStart, _ := reader.Seek(0, io.SeekCurrent)
			if bytesPerSecond := float64(decoder.SampleRate) * float64(decoder.NumChans) * float64(decoder.BitDepth/8); bytesPerSecond > 0 {
				duration = max(duration, float64(int64(len(audio))-dataStart)/bytesPerSecond)
			}
		}
	}
	if duration <= 0 {
		return 0, false
	}
	return int64(math.Ceil(min(duration, transcriptionMaxSeconds))), true
}
