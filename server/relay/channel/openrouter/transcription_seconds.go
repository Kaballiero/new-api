package openrouter

import (
	"bytes"
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// transcriptionSeconds is the duration of a transcription file in whole
// seconds, rounded up, read from its audio headers by the file extension
// (owner rule of 14.11). The headers are client-controlled, so the duration
// is capped by what the file size holds at 8 kbit/s, 1000 bytes a second,
// before it is converted; the file is at most 25 MiB, so the seconds fit
// int64. A format without a duration reader, unreadable headers or a duration
// that is not a positive finite number refuse the request.
func transcriptionSeconds(ctx context.Context, filename string, audio []byte) (int64, error) {
	duration, err := common.GetAudioDuration(ctx, bytes.NewReader(audio), strings.ToLower(filepath.Ext(filename)))
	if err != nil || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 {
		return 0, errors.New("OpenRouter transcription audio duration is unreadable")
	}
	return int64(math.Ceil(min(duration, float64(len(audio))/1000))), nil
}
