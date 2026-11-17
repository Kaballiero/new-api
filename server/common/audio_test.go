package common

import (
	"bytes"
	"context"
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

func TestGetAudioDurationAACKeepsValidADTSDuration(t *testing.T) {
	const sampling16kHzIndex = 8
	var stream []byte
	for range 10 {
		stream = append(stream, adtsFrame(sampling16kHzIndex, 200)...)
	}

	duration, err := GetAudioDuration(context.Background(), bytes.NewReader(stream), ".aac")

	require.NoError(t, err)
	assert.Equal(t, float64(10*1024)/16000, duration)
}

func TestGetAudioDurationAACRejectsMalformedStreamQuickly(t *testing.T) {
	// An MPEG-1 Layer III frame header (128 kbps, 44.1 kHz) shares the 0xFFF
	// sync word with ADTS; its zero payload decodes as a zero ADTS frame length.
	mp3Frame := make([]byte, 417)
	copy(mp3Frame, []byte{0xFF, 0xFB, 0x90, 0x64})
	mp3Stream := bytes.Repeat(mp3Frame, 2600)

	zeroLengthStream := append(adtsFrame(4, 200), adtsFrame(4, 0)...)
	truncatedStream := append(adtsFrame(4, 200), adtsFrame(4, 400)[:100]...)

	tests := []struct {
		name   string
		stream []byte
	}{
		{name: "mp3 frames named aac", stream: mp3Stream},
		{name: "zero frame length", stream: zeroLengthStream},
		{name: "frame runs past the data", stream: truncatedStream},
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
