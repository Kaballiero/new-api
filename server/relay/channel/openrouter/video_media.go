package openrouter

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/abema/go-mp4"
	"github.com/shopspring/decimal"
	codec "github.com/yapingcat/gomedia/go-codec"
)

// SourceMediaVideo identifies the narrow source-measured reservation contract.
// It does not establish a maximum upstream charge or enable URL downloads.
func SourceMediaVideo(model string) bool {
	switch model {
	case "black-forest-labs/flux-video-upscale", "heygen/avatar-iv", "runway/aleph-2":
		return true
	}
	return false
}

type videoMediaReference struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
	AudioURL struct {
		URL string `json:"url"`
	} `json:"audio_url"`
	VideoURL struct {
		URL string `json:"url"`
	} `json:"video_url"`
}

// estimateSourceMediaVideo only reads immutable inline bytes that remain in the
// cached POST. Output rounding/minimums are not fully published: callers must
// require an explicit approximate reserve even after these quantities validate.
func estimateSourceMediaVideo(model VideoModelPricing, body []byte) (float64, error) {
	var order struct {
		VideoPriceOrder
		Prompt          string                `json:"prompt"`
		InputReferences []videoMediaReference `json:"input_references"`
	}
	if err := common.Unmarshal(body, &order); err != nil {
		return 0, err
	}
	fail := func(reason string) (float64, error) {
		return 0, fmt.Errorf("OpenRouter source media quantity: %s", reason)
	}
	if order.Duration != nil || order.Size != "" || order.AspectRatio != "" || order.GenerateAudio != nil || len(order.FrameImages) > 0 {
		return fail("source media quantity must determine duration and dimensions")
	}
	for _, options := range order.Provider.Options {
		if len(options) > 0 {
			return fail("provider options are unsupported for measured media")
		}
	}
	avatar := model.ID == "heygen/avatar-iv"
	if avatar {
		if len(order.InputReferences) != 2 || order.Prompt != "" || order.UpscaleFactor != nil || order.Creativity != nil {
			return fail("Avatar requires one image and one audio; script/TTS is unsupported")
		}
		if order.Resolution != "720p" && order.Resolution != "1080p" {
			return fail("Avatar requires an explicit 720p or 1080p resolution")
		}
		if !slices.Contains(model.SupportedResolutions, order.Resolution) {
			return fail("resolution absent from fresh capabilities")
		}
	} else if len(order.InputReferences) != 1 || order.Resolution != "" {
		return fail("exactly one source video is required")
	}
	total, images, audios, videos := 0, 0, 0, 0
	seconds, width, height := 0.0, 0, 0
	for _, ref := range order.InputReferences {
		uri := ref.VideoURL.URL
		if ref.Type == "image_url" {
			uri = ref.ImageURL.URL
		} else if ref.Type == "audio_url" {
			uri = ref.AudioURL.URL
		}
		header, encoded, ok := strings.Cut(uri, ",")
		// The 16MB encoded limit also fits Runway's documented data URI limit.
		if !ok || len(uri) > 16_000_000 || total+len(uri) > 16_000_000 {
			return fail("inline media exceeds the total byte limit")
		}
		total += len(uri)
		if base64.StdEncoding.DecodedLen(len(encoded)) > 12_000_000 {
			return fail("media exceeds decoded byte limit")
		}
		data, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil || len(data) == 0 {
			return fail("invalid base64 media")
		}
		switch ref.Type {
		case "image_url":
			images++
			if !avatar || images > 1 || (header != "data:image/png;base64" && header != "data:image/jpeg;base64") {
				return fail("unsupported image input")
			}
			config, format, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil || (format != "png" && format != "jpeg") || header != "data:image/"+format+";base64" || config.Width <= 0 || config.Height <= 0 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16_000_000 {
				return fail("invalid or oversized image")
			}
		case "audio_url":
			audios++
			if !avatar || audios > 1 || header != "data:audio/wav;base64" {
				return fail("only one inline PCM WAV is supported")
			}
			// WAV's byte rate is attacker-controlled. Validate PCM framing
			// and the actual data chunk size before deriving its duration.
			if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) || string(data[12:16]) != "fmt " || binary.LittleEndian.Uint32(data[16:20]) != 16 || binary.LittleEndian.Uint16(data[20:22]) != 1 || string(data[36:40]) != "data" {
				return fail("unsupported WAV framing")
			}
			channels, rate, bits := uint64(binary.LittleEndian.Uint16(data[22:24])), uint64(binary.LittleEndian.Uint32(data[24:28])), uint64(binary.LittleEndian.Uint16(data[34:36]))
			block := channels * bits / 8
			if channels < 1 || channels > 2 || rate < 8000 || rate > 48000 || bits != 16 || uint64(binary.LittleEndian.Uint16(data[32:34])) != block || uint64(binary.LittleEndian.Uint32(data[28:32])) != rate*block || uint64(binary.LittleEndian.Uint32(data[40:44])) != uint64(len(data)-44) || uint64(len(data)-44)%block != 0 {
				return fail("invalid WAV quantities")
			}
			seconds = float64(len(data)-44) / float64(rate*block)
		case "video_url":
			videos++
			if avatar || videos > 1 || header != "data:video/mp4;base64" {
				return fail("only one inline MP4 is supported")
			}
			seconds, width, height, err = probeBoundedVideo(data)
			if err != nil {
				return 0, err
			}
		default:
			return fail("unsupported reference type")
		}
	}
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > min(30, relaycommon.MaxTaskDurationSeconds) {
		return fail("source duration must be positive and at most 30 seconds")
	}
	if avatar && (images != 1 || audios != 1) {
		return fail("Avatar requires one image and one audio")
	}
	quantity := decimal.NewFromFloat(seconds)
	divisor := decimal.NewFromInt(100)
	keys := []string{"cents_per_second_output", "minimum_cents_per_generation"}
	selected := "cents_per_second_output"
	if avatar {
		keys = []string{"duration_seconds"}
		selected = "duration_seconds"
		divisor = decimal.NewFromInt(1)
	}
	if model.ID == "black-forest-labs/flux-video-upscale" {
		if seconds > 20 || min(width, height) < 480 || max(width, height) > 2560 || min(width, height) > 1440 {
			return fail("FLUX source exceeds duration/dimension limits")
		}
		factor := 2.0
		if order.UpscaleFactor != nil {
			factor = *order.UpscaleFactor
		}
		if math.IsNaN(factor) || math.IsInf(factor, 0) || factor < 1.5 || factor > 3 {
			return fail("invalid upscale factor")
		}
		creativity := 1
		if order.Creativity != nil {
			creativity = *order.Creativity
		}
		if creativity != 0 && creativity != 1 {
			return fail("invalid creativity")
		}
		keys = []string{"cents_per_megapixel_second_precise", "cents_per_megapixel_second_creative"}
		selected = keys[creativity]
		// Nominal output area, not an asserted rounding rule or 14.4MP cap.
		quantity = quantity.Mul(decimal.NewFromInt(int64(width) * int64(height))).Mul(decimal.NewFromFloat(factor)).Mul(decimal.NewFromFloat(factor)).Div(decimal.NewFromInt(1_048_576))
	} else if !avatar {
		if seconds < 2 || max(width, height) > 1920 || min(width, height) > 1080 || order.UpscaleFactor != nil || order.Creativity != nil {
			return fail("Aleph requires 2–30 seconds at up to 1080p")
		}
	}
	rates := map[string]decimal.Decimal{}
	for key, raw := range model.PricingSKUs {
		if !slices.Contains(keys, key) {
			return fail("unknown measured-media tariff: " + key)
		}
		rate, err := decimal.NewFromString(raw)
		number, _ := rate.Float64()
		if err != nil || rate.IsNegative() || math.IsNaN(number) || math.IsInf(number, 0) {
			return fail("invalid measured-media tariff")
		}
		rates[key] = rate
	}
	rate, ok := rates[selected]
	if !ok {
		return fail("missing measured-media tariff")
	}
	cost := quantity.Mul(rate).Div(divisor)
	if floor, ok := rates["minimum_cents_per_generation"]; ok {
		cost = decimal.Max(cost, floor.Div(decimal.NewFromInt(100)))
	}
	value, _ := cost.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fail("invalid media estimate")
	}
	return value, nil
}

// probeBoundedVideo accepts a single silent, nonfragmented AVC track without
// edit lists or reordered frames. Container and sample quantities must agree.
func probeBoundedVideo(data []byte) (seconds float64, width, height int, err error) {
	invalid := errors.New("OpenRouter source media: invalid or unsupported MP4 quantities")
	defer func() {
		if recover() != nil {
			seconds, width, height, err = 0, 0, 0, invalid
		}
	}()
	reader := bytes.NewReader(data)
	boxes, sampleCount := 0, uint64(0)
	descriptions := 0
	var mdatStart, mdatEnd uint64
	_, err = mp4.ReadBoxStructure(reader, func(h *mp4.ReadHandle) (any, error) {
		boxes++
		if boxes > 128 || len(h.Path) > 8 || h.BoxInfo.Offset > uint64(len(data)) || h.BoxInfo.Size > uint64(len(data))-h.BoxInfo.Offset {
			return nil, invalid
		}
		if len(h.Path) > 1 && h.Path[len(h.Path)-2] == mp4.BoxTypeStsd() {
			descriptions++
			if descriptions != 1 || h.BoxInfo.Type != mp4.BoxTypeAvc1() {
				return nil, invalid
			}
		}
		switch h.BoxInfo.Type.String() {
		case "moof", "mvex", "edts", "ctts":
			return nil, invalid
		case "moov", "trak", "mdia", "minf", "stbl":
			return h.Expand()
		case "stts":
			if h.BoxInfo.Size > 8192 {
				return nil, invalid
			}
			payload, _, e := h.ReadPayload()
			if e != nil {
				return nil, e
			}
			for _, entry := range payload.(*mp4.Stts).Entries {
				sampleCount += uint64(entry.SampleCount)
				if sampleCount > 900 || entry.SampleDelta == 0 {
					return nil, invalid
				}
			}
		case "stsd", "stsz", "stsc", "stco", "co64":
			if h.BoxInfo.Size > 65536 {
				return nil, invalid
			}
			payload, _, e := h.ReadPayload()
			if e != nil {
				return nil, e
			}
			switch box := payload.(type) {
			case *mp4.Stsd:
				if box.EntryCount != 1 {
					return nil, invalid
				}
				return h.Expand()
			case *mp4.Stsc:
				for _, entry := range box.Entries {
					if entry.SampleDescriptionIndex != 1 || entry.SamplesPerChunk == 0 || entry.SamplesPerChunk > 900 {
						return nil, invalid
					}
				}
			}
		case "mdat":
			if mdatEnd != 0 {
				return nil, invalid
			}
			mdatStart = h.BoxInfo.Offset + h.BoxInfo.HeaderSize
			mdatEnd = h.BoxInfo.Offset + h.BoxInfo.Size
		}
		return nil, nil
	})
	if err != nil || sampleCount == 0 || mdatEnd == 0 || descriptions != 1 {
		return 0, 0, 0, invalid
	}
	info, e := mp4.Probe(reader)
	if e != nil || info.Timescale == 0 || len(info.Tracks) != 1 || len(info.Segments) > 0 {
		return 0, 0, 0, invalid
	}
	track := info.Tracks[0]
	if track.Timescale == 0 || track.Codec != mp4.CodecAVC1 || track.Encrypted || track.AVC == nil || len(track.EditList) > 0 || uint64(len(track.Samples)) != sampleCount {
		return 0, 0, 0, invalid
	}
	config, e := mp4.ExtractBoxesWithPayload(reader, nil, []mp4.BoxPath{{mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), mp4.BoxTypeMinf(), mp4.BoxTypeStbl(), mp4.BoxTypeStsd(), mp4.BoxTypeAvc1(), mp4.BoxTypeAvcC()}})
	if e != nil || len(config) != 1 {
		return 0, 0, 0, invalid
	}
	avcc := config[0].Payload.(*mp4.AVCDecoderConfiguration)
	if avcc.LengthSizeMinusOne != 3 || len(avcc.SequenceParameterSets) != 1 || len(avcc.PictureParameterSets) != 1 {
		return 0, 0, 0, invalid
	}
	sw, sh, e := boundedBaselineSPS(avcc.SequenceParameterSets[0].NALUnit)
	if e != nil || sw != int(track.AVC.Width) || sh != int(track.AVC.Height) {
		return 0, 0, 0, invalid
	}
	width, height = int(track.AVC.Width), int(track.AVC.Height)
	if width <= 0 || height <= 0 || width > 2560 || height > 2560 {
		return 0, 0, 0, invalid
	}
	var ticks uint64
	for _, sample := range track.Samples {
		if sample.TimeDelta == 0 || uint64(sample.TimeDelta)*30 < uint64(track.Timescale) || sample.Size == 0 || sample.CompositionTimeOffset != 0 {
			return 0, 0, 0, invalid
		}
		ticks += uint64(sample.TimeDelta)
	}
	seconds = float64(ticks) / float64(track.Timescale)
	if ticks != track.Duration || seconds != float64(info.Duration)/float64(info.Timescale) || seconds <= 0 || seconds > min(30, relaycommon.MaxTaskDurationSeconds) {
		return 0, 0, 0, invalid
	}
	sampleIndex := 0
	previousEnd, scanned := mdatStart, uint64(0)
	for _, chunk := range track.Chunks {
		offset := chunk.DataOffset
		if chunk.SamplesPerChunk == 0 {
			return 0, 0, 0, invalid
		}
		for range chunk.SamplesPerChunk {
			if sampleIndex >= len(track.Samples) {
				return 0, 0, 0, invalid
			}
			size := uint64(track.Samples[sampleIndex].Size)
			sampleIndex++
			if offset < previousEnd || offset >= mdatEnd || size > mdatEnd-offset || size > uint64(len(data))-scanned {
				return 0, 0, 0, invalid
			}
			// Reject in-band parameter sets that could replace validated dimensions.
			packet := data[offset : offset+size]
			for len(packet) > 0 {
				if len(packet) < 5 {
					return 0, 0, 0, invalid
				}
				n := uint64(binary.BigEndian.Uint32(packet[:4]))
				if n == 0 || n > uint64(len(packet)-4) {
					return 0, 0, 0, invalid
				}
				kind := packet[4] & 31
				if kind != 1 && kind != 5 && kind != 6 && kind != 9 {
					return 0, 0, 0, invalid
				}
				packet = packet[4+n:]
			}
			offset += size
			previousEnd = offset
			scanned += size
		}
	}
	if sampleIndex != len(track.Samples) {
		return 0, 0, 0, invalid
	}
	return seconds, width, height, nil
}

// A baseline-only SPS avoids complex codec extensions in this initial contract.
// Parse just the bounded fields through cropping using the existing bit reader;
// the general SPS decoder allocates from untrusted HRD counts in optional VUI.
func boundedBaselineSPS(nal []byte) (width, height int, err error) {
	invalid := errors.New("unsupported or malformed baseline SPS")
	defer func() {
		if recover() != nil {
			width, height, err = 0, 0, invalid
		}
	}()
	if len(nal) < 5 || len(nal) > 256 || nal[0] != 0x67 || nal[1] != 66 {
		return 0, 0, invalid
	}
	bs := codec.NewBitStream(codec.CovertRbspToSodb(nal[1:]))
	bs.SkipBits(24)
	if bs.ReadUE() > 31 || bs.ReadUE() > 12 {
		return 0, 0, invalid
	}
	poc := bs.ReadUE()
	if poc != 0 && poc != 2 {
		return 0, 0, invalid
	}
	if poc == 0 && bs.ReadUE() > 12 {
		return 0, 0, invalid
	}
	if bs.ReadUE() > 16 {
		return 0, 0, invalid
	}
	bs.SkipBits(1)
	w, h := bs.ReadUE(), bs.ReadUE()
	if w >= 160 || h >= 160 || bs.GetBit() != 1 {
		return 0, 0, invalid
	}
	bs.SkipBits(1)
	left, right, top, bottom := uint64(0), uint64(0), uint64(0), uint64(0)
	if bs.GetBit() == 1 {
		left, right, top, bottom = bs.ReadUE(), bs.ReadUE(), bs.ReadUE(), bs.ReadUE()
	}
	if left > 2048 || right > 2048 || top > 2048 || bottom > 2048 || 2*(left+right) >= (w+1)*16 || 2*(top+bottom) >= (h+1)*16 {
		return 0, 0, invalid
	}
	return int((w+1)*16 - 2*(left+right)), int((h+1)*16 - 2*(top+bottom)), nil
}
