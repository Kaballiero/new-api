package openrouter

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/app/pricingruntime"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/estimate_upstream_video_reserve"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type VideoPriceOrder struct {
	Model         string   `json:"model"`
	Duration      *int     `json:"duration"`
	Resolution    string   `json:"resolution"`
	AspectRatio   string   `json:"aspect_ratio"`
	Size          string   `json:"size"`
	GenerateAudio *bool    `json:"generate_audio"`
	UpscaleFactor *float64 `json:"upscale_factor"`
	Creativity    *int     `json:"creativity"`
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

// UsageFacts describes the prepared order, never measurements of source media.
// Missing scalar quantities stay absent so expressions cannot price them as zero.
func (order VideoPriceOrder) UsageFacts() map[string]any {
	images, videos, audios := len(order.FrameImages), 0, 0
	for _, reference := range order.InputReferences {
		switch reference.Type {
		case "image_url":
			images++
		case "video_url":
			videos++
		case "audio_url":
			audios++
		}
	}
	mode := "text_to_video"
	if len(order.FrameImages) > 0 {
		mode = "image_to_video"
	} else if len(order.InputReferences) > 0 {
		mode = "reference_to_video"
	}
	facts := map[string]any{"images": images, "frame_images": len(order.FrameImages), "reference_images": images - len(order.FrameImages), "videos": videos, "audios": audios, "jobs": 1, "generation_mode": mode, "audio": "default", "creativity": "default"}
	for key, value := range map[string]string{"resolution": order.Resolution, "size": order.Size, "aspect_ratio": order.AspectRatio} {
		if value == "" {
			value = "default"
		}
		facts[key] = value
	}
	if order.Duration != nil {
		facts["requested_seconds"] = *order.Duration
	}
	if order.GenerateAudio != nil {
		if *order.GenerateAudio {
			facts["audio"] = "enabled"
		} else {
			facts["audio"] = "disabled"
		}
	}
	if order.UpscaleFactor != nil {
		facts["upscale_factor"] = *order.UpscaleFactor
	}
	if order.Creativity != nil {
		facts["creativity"] = strconv.Itoa(*order.Creativity)
	}
	return facts
}

// EstimateVideo prices the descriptor cached by plugin validation from the
// video tariff of its model (estimate_upstream_video_reserve: the fresh
// upstream catalog, otherwise the live /videos/models). It never fetches a
// client media URL or changes the POST.
func EstimateVideo(c *gin.Context, info *relaycommon.RelayInfo, body []byte) (estimate_upstream_video_reserve.Estimate, error) {
	var order VideoPriceOrder
	if err := common.Unmarshal(body, &order); err != nil || order.Model != info.UpstreamModelName {
		return estimate_upstream_video_reserve.Estimate{}, errors.New("OpenRouter reservation unavailable: invalid mapped video model")
	}
	estimate, err := pricingruntime.EstimateUpstreamVideoReserve().Execute(estimate_upstream_video_reserve.Input{
		Model:          order.Model,
		ChannelBaseURL: info.ChannelBaseUrl,
		Live:           liveVideoTariffs{c: c, info: info},
		Order:          videoOrder{order: order, body: body, info: info},
	})
	if err != nil {
		return estimate_upstream_video_reserve.Estimate{}, err
	}
	if estimate.Source == pricingdomain.ReserveSourceCatalogStale {
		logger.LogWarn(c, "OpenRouter video reserve priced from a stale upstream catalog: model=%s", order.Model)
	}
	return estimate, nil
}

// videoOrder is the order port of estimate_upstream_video_reserve: the
// prepared descriptor priced by the documented SKU rules (EstimateVideoOrder),
// or by its inline source media under the channel reserve of the model.
type videoOrder struct {
	order VideoPriceOrder
	body  []byte
	info  *relaycommon.RelayInfo
}

func (o videoOrder) Price(model pricingdomain.VideoModelPricing) (float64, error) {
	if !SourceMediaVideo(o.order.Model) {
		return EstimateVideoOrder(model, o.order)
	}
	estimate, err := estimateSourceMediaVideo(model, o.body)
	if err != nil {
		return 0, err
	}
	// HeyGen requires a public audio URL or an uploaded asset. OR's
	// normalized audio reference schema does not prove inline upload.
	if o.order.Model == "heygen/avatar-iv" {
		return 0, errors.New("OpenRouter Avatar inline audio transport is unconfirmed; source media remains disabled")
	}
	reserve := o.info.ChannelSetting.OpenRouterReserveUSD(o.order.Model)
	if reserve == nil || *reserve <= 0 || math.IsNaN(*reserve) || math.IsInf(*reserve, 0) {
		return 0, errors.New("OpenRouter source media requires an explicit approximate openrouter_model_reserve_usd; output rounding is not a proven spending bound")
	}
	return max(estimate, *reserve), nil
}

var videoSKUVariant = regexp.MustCompile(`^(?:(text_to_video|image_to_video)_)?(duration_seconds|long_duration_seconds|reference_duration_seconds|video_tokens|cents_per_video_output_second|cents_per_second_output|cents_per_second_video_continuation)(?:_(with_audio|without_audio))?(?:_(360p|480p|720p|768p|1080p|2k|4k))?(?:_(with_video_input))?$`)

// heyGenLongDurationThresholdSeconds is the output length above which OpenRouter
// bills heygen/heygen-video-1 at long_duration_seconds ("Video Output (over 15 s)").
const heyGenLongDurationThresholdSeconds = 15

// EstimateVideoOrder implements only documented monetary SKU families. Unknown
// quantities and SKU units fail admission instead of falling back to a tariff.
func EstimateVideoOrder(model pricingdomain.VideoModelPricing, order VideoPriceOrder) (float64, error) {
	fail := func(reason string) (float64, error) {
		return 0, fmt.Errorf("OpenRouter reservation unavailable: %s", reason)
	}
	if model.ID != order.Model || len(model.PricingSKUs) == 0 {
		return fail("missing operation tariff")
	}
	// Unit/quantity contracts select the calculation. Model names never admit
	// a monetary family; the few rules below fill gaps in public discovery.
	videoTokens, referenceOutput, continuationOutput := false, false, false
	quantityUnit := ""
	for key := range model.PricingSKUs {
		variant := videoSKUVariant.FindStringSubmatch(key)
		if variant == nil {
			continue
		}
		unit := "second"
		if variant[2] == "video_tokens" {
			unit = "token"
		}
		if quantityUnit != "" && quantityUnit != unit {
			return fail("incompatible video output quantity units")
		}
		quantityUnit = unit
		videoTokens = videoTokens || variant[2] == "video_tokens"
		referenceOutput = referenceOutput || variant[2] == "reference_duration_seconds"
		continuationOutput = continuationOutput || variant[2] == "cents_per_second_video_continuation"
	}
	// Discovery exposes a token price, but not its pixel/FPS meter. Do not
	// extend the documented 24fps/1024px quantity contract to unknown models.
	if videoTokens && !slices.Contains([]string{"bytedance/seedance-2.0", "bytedance/seedance-2.0-mini", "bytedance/seedance-2.0-fast", "bytedance/seedance-1-5-pro", "bytedance/seedance-2.5"}, model.ID) {
		return fail("video token quantity contract is unavailable")
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
	if model.ID == "heygen/heygen-video-1" {
		if len(order.FrameImages) > 1 || len(order.FrameImages) > 0 && len(order.InputReferences) > 0 || referenceImages > 9 || videos > 3 || audios > 3 || len(order.InputReferences) > 12 {
			return fail("unsupported HeyGen reference quantities")
		}
		if referenceImages > 2 {
			if _, ok := model.PricingSKUs["reference_input_images"]; !ok {
				return fail("missing reference image tariff")
			}
		}
	}
	// Discovery omits the image surcharge requirement and the free allowances.
	// These documented compatibility rules do not select the output calculation.
	if slices.Contains([]string{"x-ai/grok-imagine-video", "x-ai/grok-imagine-video-1.5", "x-ai/grok-imagine-video-1.5-lite"}, model.ID) && images > 0 {
		if _, ok := model.PricingSKUs["cents_per_image_input"]; !ok {
			return fail("missing image input tariff")
		}
	}
	if model.ID == "minimax/hailuo-3" && images > 5 {
		if _, ok := model.PricingSKUs["reference_images"]; !ok {
			return fail("missing reference image tariff")
		}
	}
	if videoTokens && videos > 0 {
		return fail("source video token quantity is unavailable")
	}
	if audios > 0 {
		return fail("audio reference tariff is unavailable")
	}
	// Only options documented as nonmonetary are admitted here. The plugin's
	// broader protocol allowlist does not establish their pricing semantics.
	for _, options := range order.Provider.Options {
		for key, value := range options {
			// Output containers can carry a surcharge (e.g. Runway ProRes/HDR).
			if key == "output_format" && value != "mp4" {
				return fail("unpriced output format")
			}
			if !slices.Contains([]string{"watermark", "req_key", "negative_prompt", "cfg_scale", "output_format"}, key) {
				return fail("unpriced provider option: " + key)
			}
		}
	}
	duration := 0
	if order.Duration != nil {
		duration = *order.Duration
		if len(model.SupportedDurations) > 0 && !slices.Contains(model.SupportedDurations, duration) {
			return fail("duration absent from fresh capabilities")
		}
	} else {
		for _, d := range model.SupportedDurations {
			duration = max(duration, d)
		}
	}
	if model.ID == "black-forest-labs/flux-video-edit" {
		duration = max(duration, 15)
	}
	if duration <= 0 || duration > relaycommon.MaxTaskDurationSeconds {
		return fail("bounded output duration is unavailable: source media quantity is required")
	}
	// HeyGen reference mode supports 5–15 seconds, while discovery lists the
	// longer durations without the generation mode.
	heyGenReferenceMode := model.ID == "heygen/heygen-video-1" && len(order.InputReferences) > 0
	explicitLongDuration := order.Duration != nil && duration > heyGenLongDurationThresholdSeconds
	if heyGenReferenceMode && explicitLongDuration {
		return fail("HeyGen reference mode supports 5–15 seconds")
	}
	if heyGenReferenceMode {
		duration = min(duration, heyGenLongDurationThresholdSeconds)
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
	if order.Size == "" && videoTokens {
		sizes = model.SupportedSizes
	}
	if len(sizes) == 0 {
		return fail("finite output size is unavailable")
	}
	maxCost := decimal.Zero
	haveOutput := false
	additive := decimal.Zero
	minimum := decimal.Zero
	for key, raw := range model.PricingSKUs {
		rate, err := decimal.NewFromString(raw)
		number, _ := rate.Float64()
		if err != nil || rate.IsNegative() || math.IsNaN(number) || math.IsInf(number, 0) {
			return fail("invalid video tariff")
		}
		if key == "cents_per_image_input" {
			additive = additive.Add(rate.Mul(decimal.NewFromInt(int64(images))).Div(decimal.NewFromInt(100)))
			continue
		}
		if key == "reference_images" && model.ID == "minimax/hailuo-3" {
			additive = additive.Add(rate.Mul(decimal.NewFromInt(int64(max(images-5, 0)))))
			continue
		}
		if key == "reference_input_images" && model.ID == "heygen/heygen-video-1" {
			additive = additive.Add(rate.Mul(decimal.NewFromInt(int64(max(referenceImages-2, 0)))))
			continue
		}
		if key == "minimum_cents_per_generation" {
			minimum = rate.Div(decimal.NewFromInt(100))
			continue
		}
		variant := videoSKUVariant.FindStringSubmatch(key)
		if variant == nil {
			return fail("unknown video pricing SKU: " + key)
		}
		mode, unit, audio, resolution, source := variant[1], variant[2], variant[3], variant[4], variant[5]
		if unit == "long_duration_seconds" {
			// The threshold is absent from discovery, so the SKU stays unknown
			// for any other model.
			if model.ID != "heygen/heygen-video-1" {
				return fail("unknown video pricing SKU: " + key)
			}
			if duration <= heyGenLongDurationThresholdSeconds {
				continue
			}
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
		if referenceOutput && (unit == "reference_duration_seconds") != (len(order.InputReferences) > 0) {
			continue
		}
		if continuationOutput && (unit == "cents_per_second_video_continuation") != (videos > 0) {
			continue
		}
		for _, size := range sizes {
			quantity := decimal.NewFromInt(int64(duration))
			if unit == "video_tokens" {
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
	total := maxCost.Add(additive)
	if minimum.GreaterThan(total) {
		total = minimum
	}
	usd, _ := total.Float64()
	if math.IsNaN(usd) || math.IsInf(usd, 0) {
		return fail("video estimate overflow")
	}
	return usd, nil
}
