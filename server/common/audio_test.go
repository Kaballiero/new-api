package common

import (
	"bytes"
	"context"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adtsFrame builds one MPEG-4 AAC-LC stereo ADTS frame without CRC whose
// header declares frameLength bytes; the payload is zero-filled.
func adtsFrame(samplingFrequencyIndex byte, frameLength int) []byte {
	const channelConfiguration = 2
	frame := make([]byte, max(frameLength, 7))
	frame[0] = 0xFF
	frame[1] = 0xF1
	frame[2] = 1<<6 | samplingFrequencyIndex<<2 | channelConfiguration>>2
	frame[3] = channelConfiguration<<6 | byte(frameLength>>11)&0x03
	frame[4] = byte(frameLength >> 3)
	frame[5] = byte(frameLength&0x07)<<5 | 0x1F
	frame[6] = 0xFC
	return frame
}

func TestGetAudioDurationAACCountsValidFramesAroundJunk(t *testing.T) {
	const sampling16kHzIndex = 8
	var frames []byte
	for range 10 {
		frames = append(frames, adtsFrame(sampling16kHzIndex, 200)...)
	}
	id3v1Tag := append([]byte("TAG"), make([]byte, 125)...)

	tests := []struct {
		name   string
		stream []byte
	}{
		{name: "frames only", stream: frames},
		{name: "junk byte mid-stream", stream: slices.Concat(frames[:1000], []byte{0x00}, frames[1000:])},
		{name: "leading false sync word", stream: slices.Concat(adtsFrame(sampling16kHzIndex, 8000)[:adtsHeaderSize], frames)},
		{name: "leading reserved sampling frequency index", stream: slices.Concat(adtsFrame(15, 200), frames)},
		{name: "id3v1 tag tail", stream: slices.Concat(frames, id3v1Tag)},
		{name: "zero frame length tail", stream: slices.Concat(frames, adtsFrame(sampling16kHzIndex, 0))},
		{name: "truncated frame tail", stream: slices.Concat(frames, adtsFrame(sampling16kHzIndex, 400)[:100])},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration, err := GetAudioDuration(context.Background(), bytes.NewReader(tt.stream), ".aac")

			require.NoError(t, err)
			assert.Equal(t, float64(10*1024)/16000, duration)
		})
	}
}

func TestGetAudioDurationAACRejectsMalformedStreamQuickly(t *testing.T) {
	// An MPEG-1 Layer III frame header (128 kbps, 44.1 kHz) shares the 0xFFF
	// sync word with ADTS but has a non-zero layer.
	mp3Frame := make([]byte, 417)
	copy(mp3Frame, []byte{0xFF, 0xFB, 0x90, 0x64})
	mp3Stream := bytes.Repeat(mp3Frame, 2600)

	tests := []struct {
		name   string
		stream []byte
	}{
		{name: "mp3 frames named aac", stream: mp3Stream},
		{name: "zero frame length", stream: append(adtsFrame(4, 0), mp3Stream...)},
		{name: "frame runs past the data", stream: adtsFrame(4, 400)[:100]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parseErr := make(chan error, 1)
			go func() {
				_, err := GetAudioDuration(context.Background(), bytes.NewReader(tt.stream), ".aac")
				parseErr <- err
			}()

			select {
			case err := <-parseErr:
				require.Error(t, err)
			case <-time.After(5 * time.Second):
				require.FailNow(t, "aac duration scan did not finish within 5s")
			}
		})
	}
}
