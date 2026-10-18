package openrouter

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type VideoModelPricing struct {
	ID                    string            `json:"id"`
	PricingSKUs           map[string]string `json:"pricing_skus"`
	SupportedDurations    []int             `json:"supported_durations"`
	SupportedSizes        []string          `json:"supported_sizes"`
	SupportedResolutions  []string          `json:"supported_resolutions"`
	SupportedAspectRatios []string          `json:"supported_aspect_ratios"`
}

type VideoPriceOrder struct {
	Model         string `json:"model"`
	Duration      *int   `json:"duration"`
	Resolution    string `json:"resolution"`
	AspectRatio   string `json:"aspect_ratio"`
	Size          string `json:"size"`
	GenerateAudio *bool  `json:"generate_audio"`
	FrameImages   []struct {
		Type string `json:"type"`
	} `json:"frame_images"`
	InputReferences []struct {
		Type string `json:"type"`
	} `json:"input_references"`
	Provider struct {
		Options map[string]map[string]any `json:"options"`
	} `json:"provider"`
}

// EstimateVideo prices the descriptor cached by plugin validation, using fresh
// operation metadata. It never fetches a client media URL or changes the POST.
func EstimateVideo(c *gin.Context, info *relaycommon.RelayInfo, body []byte) (float64, error) {
	var order VideoPriceOrder
	if err := common.Unmarshal(body, &order); err != nil || order.Model != info.UpstreamModelName {
		return 0, errors.New("OpenRouter reservation unavailable: invalid mapped video model")
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, strings.TrimRight(info.ChannelBaseUrl, "/")+"/v1/videos/models", nil)
	if err != nil {
		return 0, errors.New("OpenRouter reservation unavailable: invalid video discovery URL")
	}
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return 0, err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return 0, errors.New("OpenRouter reservation unavailable: video discovery transport")
	}
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return 0, errors.New("OpenRouter reservation unavailable: video discovery failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("OpenRouter reservation unavailable: video discovery HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(data) > 4<<20 {
		return 0, errors.New("OpenRouter reservation unavailable: invalid video discovery response")
	}
	var catalog struct {
		Data []VideoModelPricing `json:"data"`
	}
	if err := common.Unmarshal(data, &catalog); err != nil {
		return 0, errors.New("OpenRouter reservation unavailable: invalid video catalog")
	}
	for _, model := range catalog.Data {
		if model.ID == order.Model {
			return EstimateVideoOrder(model, order)
		}
	}
	return 0, errors.New("OpenRouter reservation unavailable: model absent from fresh video catalog")
}

var videoSKUVariant = regexp.MustCompile(`^(?:(text_to_video|image_to_video)_)?(duration_seconds|reference_duration_seconds|video_tokens|cents_per_video_output_second|cents_per_second_output|cents_per_second_video_continuation)(?:_(with_audio|without_audio))?(?:_(480p|720p|768p|1080p|2k|4k))?(?:_(with_video_input))?$`)

// EstimateVideoOrder implements only documented monetary SKU families. Unknown
// quantities and SKU units fail admission instead of falling back to a tariff.
func EstimateVideoOrder(model VideoModelPricing, order VideoPriceOrder) (float64, error) {
	fail := func(reason string) (float64, error) {
		return 0, fmt.Errorf("OpenRouter reservation unavailable: %s", reason)
	}
	if model.ID != order.Model || len(model.PricingSKUs) == 0 {
		return fail("missing operation tariff")
	}
	if slices.Contains([]string{"heygen/avatar-iv", "runway/aleph-2", "black-forest-labs/flux-video-upscale"}, model.ID) {
		return fail("source media quantity is unavailable for " + model.ID)
	}
	family := ""
	switch {
	case slices.Contains([]string{"x-ai/grok-imagine-video", "x-ai/grok-imagine-video-1.5", "x-ai/grok-imagine-video-1.5-lite"}, model.ID):
		family = "grok"
	case model.ID == "heygen/heygen-video-1":
		family = "heygen"
	case model.ID == "minimax/hailuo-3":
		family = "hailuo3"
	case slices.Contains([]string{"black-forest-labs/flux-3-video", "runway/gen-4.5", "black-forest-labs/flux-video-edit"}, model.ID):
		family = "cents"
	case slices.Contains([]string{"bytedance/seedance-2.0", "bytedance/seedance-2.0-mini", "bytedance/seedance-2.0-fast", "bytedance/seedance-1-5-pro", "bytedance/seedance-2.5"}, model.ID):
		family = "tokens"
	case slices.Contains([]string{"minimax/hailuo-3-max", "alibaba/wan-3.0", "alibaba/wan-3.0-prime", "alibaba/happyhorse-1.0", "alibaba/happyhorse-1.1", "kwaivgi/kling-v3.0-pro", "kwaivgi/kling-v3.0-std", "google/veo-3.1", "google/veo-3.1-fast", "google/veo-3.1-lite", "kwaivgi/kling-video-o1", "minimax/hailuo-2.3", "alibaba/wan-2.7", "alibaba/wan-2.6"}, model.ID):
		family = "seconds"
	default:
		return fail("unknown video monetary model family")
	}
	images, videos, audios := len(order.FrameImages), 0, 0
	for _, ref := range order.InputReferences {
		switch ref.Type {
		case "image_url":
			images++
		case "video_url":
			videos++
		case "audio_url":
			audios++
		default:
			return fail("unknown input reference quantity")
		}
	}
	referenceImages := images - len(order.FrameImages)
	if family == "heygen" {
		if len(order.FrameImages) > 1 || len(order.FrameImages) > 0 && len(order.InputReferences) > 0 || referenceImages > 9 || videos > 3 || audios > 3 || len(order.InputReferences) > 12 {
			return fail("unsupported HeyGen reference quantities")
		}
		if referenceImages > 2 {
			if _, ok := model.PricingSKUs["reference_input_images"]; !ok {
				return fail("missing reference image tariff")
			}
		}
	}
	if family == "grok" && images > 0 {
		if _, ok := model.PricingSKUs["cents_per_image_input"]; !ok {
			return fail("missing image input tariff")
		}
	}
	if family == "hailuo3" && images > 5 {
		if _, ok := model.PricingSKUs["reference_images"]; !ok {
			return fail("missing reference image tariff")
		}
	}
	if family == "tokens" && videos > 0 {
		return fail("Seedance source video token quantity is unavailable")
	}
	if audios > 0 {
		return fail("audio reference tariff is unavailable")
	}
	// Only options documented as nonmonetary are admitted here. The plugin's
	// broader protocol allowlist does not establish their pricing semantics.
	for _, options := range order.Provider.Options {
		for key := range options {
			if !slices.Contains([]string{"watermark", "req_key", "negative_prompt", "cfg_scale", "output_format"}, key) {
				return fail("unpriced provider option: " + key)
			}
		}
	}
	duration := 0
	if order.Duration != nil {
		duration = *order.Duration
		if !slices.Contains(model.SupportedDurations, duration) {
			return fail("duration absent from fresh capabilities")
		}
	} else {
		for _, d := range model.SupportedDurations {
			duration = max(duration, d)
		}
	}
	if model.ID == "black-forest-labs/flux-video-edit" {
		duration = 15
	}
	if duration <= 0 || duration > relaycommon.MaxTaskDurationSeconds {
		return fail("bounded output duration is unavailable")
	}
	if order.Resolution != "" && !slices.Contains(model.SupportedResolutions, order.Resolution) {
		return fail("resolution absent from fresh capabilities")
	}
	if order.AspectRatio != "" && !slices.Contains(model.SupportedAspectRatios, order.AspectRatio) {
		return fail("aspect ratio absent from fresh capabilities")
	}
	sizes := []string{order.Size}
	if order.Size != "" && !slices.Contains(model.SupportedSizes, order.Size) {
		return fail("size absent from fresh capabilities")
	}
	if order.Size == "" && family == "tokens" {
		sizes = model.SupportedSizes
	}
	if len(sizes) == 0 {
		return fail("finite output size is unavailable")
	}
	maxCost := decimal.Zero
	haveOutput := false
	additive := decimal.Zero
	for key, raw := range model.PricingSKUs {
		rate, err := decimal.NewFromString(raw)
		number, _ := rate.Float64()
		if err != nil || rate.IsNegative() || math.IsNaN(number) || math.IsInf(number, 0) {
			return fail("invalid video tariff")
		}
		if key == "cents_per_image_input" && family == "grok" {
			additive = additive.Add(rate.Mul(decimal.NewFromInt(int64(images))).Div(decimal.NewFromInt(100)))
			continue
		}
		if key == "reference_images" && family == "hailuo3" {
			additive = additive.Add(rate.Mul(decimal.NewFromInt(int64(max(images-5, 0)))))
			continue
		}
		if key == "reference_input_images" && family == "heygen" {
			additive = additive.Add(rate.Mul(decimal.NewFromInt(int64(max(referenceImages-2, 0)))))
			continue
		}
		variant := videoSKUVariant.FindStringSubmatch(key)
		if variant == nil {
			return fail("unknown video pricing SKU: " + key)
		}
		mode, unit, audio, resolution, source := variant[1], variant[2], variant[3], variant[4], variant[5]
		allowed := family == "grok" && unit == "cents_per_video_output_second" || family == "seconds" && unit == "duration_seconds" || family == "hailuo3" && unit == "duration_seconds" || family == "heygen" && (unit == "duration_seconds" || unit == "reference_duration_seconds") || family == "tokens" && unit == "video_tokens" || family == "cents" && (unit == "cents_per_second_output" || unit == "cents_per_second_video_continuation")
		if !allowed {
			return fail("inconsistent video pricing SKU: " + key)
		}
		if mode == "text_to_video" && images > 0 || mode == "image_to_video" && images == 0 {
			continue
		}
		if order.GenerateAudio != nil && (audio == "with_audio" && !*order.GenerateAudio || audio == "without_audio" && *order.GenerateAudio) {
			continue
		}
		if resolution != "" && order.Resolution != "" && !strings.EqualFold(resolution, order.Resolution) {
			continue
		}
		if source != "" && videos == 0 {
			continue
		}
		if family == "heygen" && (unit == "reference_duration_seconds") != (len(order.InputReferences) > 0) {
			continue
		}
		if model.ID == "black-forest-labs/flux-3-video" && (unit == "cents_per_second_video_continuation") != (videos > 0) {
			continue
		}
		for _, size := range sizes {
			quantity := decimal.NewFromInt(int64(duration))
			if family == "tokens" {
				widthText, heightText, ok := strings.Cut(size, "x")
				width, e1 := strconv.Atoi(widthText)
				height, e2 := strconv.Atoi(heightText)
				if !ok || e1 != nil || e2 != nil || width <= 0 || height <= 0 || width > 16384 || height > 16384 {
					return fail("invalid finite output size")
				}
				// Aspect-ratio rounding in published sizes is tolerated by one percent.
				if order.AspectRatio != "" {
					a, b, ok := strings.Cut(order.AspectRatio, ":")
					aw, e1 := strconv.Atoi(a)
					ah, e2 := strconv.Atoi(b)
					if !ok || e1 != nil || e2 != nil || aw <= 0 || ah <= 0 {
						return fail("invalid aspect ratio")
					}
					if math.Abs(float64(width)*float64(ah)/(float64(height)*float64(aw))-1) > 0.01 {
						continue
					}
				}
				quantity = decimal.NewFromInt(int64(width)).Mul(decimal.NewFromInt(int64(height))).Mul(quantity).Mul(decimal.NewFromInt(24)).Div(decimal.NewFromInt(1024)).Ceil()
			}
			cost := rate.Mul(quantity)
			if strings.HasPrefix(unit, "cents_") {
				cost = cost.Div(decimal.NewFromInt(100))
			}
			if !haveOutput || cost.GreaterThan(maxCost) {
				maxCost = cost
			}
			haveOutput = true
		}
	}
	if !haveOutput {
		return fail("no compatible output tariff")
	}
	total, _ := maxCost.Add(additive).Float64()
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return fail("video estimate overflow")
	}
	return total, nil
}
