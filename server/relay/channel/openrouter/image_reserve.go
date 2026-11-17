package openrouter

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// imageTierSides are the sides in pixels of the resolution tiers a request
// names. Tariff variants name them in lower case, alone ("1k") or as the last
// part of a variant ("low_1k").
var imageTierSides = map[string]int{"512": 512, "768": 768, "1K": 1024, "1.5K": 1536, "2K": 2048, "4K": 4096}

// unboundedImageMegapixels sizes the output of a model that publishes no
// resolution tiers when the request names no size.
const unboundedImageMegapixels = 16.0

// EstimateImageCost reserves an estimate of an image request from the tariffs
// of its endpoint; it is never substituted for actual usage.cost, which
// reconciles it.
//
// The requested tier is the resolution, or for an explicit WxH size the
// smallest tier whose side covers the longer side. Each billable unit takes
// the highest variant without a requested tier. With one, it takes the
// smallest published resolution variant not below the tier, or the highest
// variant when none is, and the variants that name no resolution
// ("high_resolution", "style_references", none) when they are higher. The
// output is W×H megapixels for an explicit size, the square of the tier side
// for a tier, otherwise the square of the largest tier the endpoint publishes,
// or unboundedImageMegapixels when it publishes none; a token is priced as
// 4096 per megapixel.
func EstimateImageCost(endpoint ImageEndpoint, count, references int, payload map[string]any) (float64, error) {
	if len(endpoint.Pricing) == 0 {
		return 0, errors.New("OpenRouter image endpoint has no published tariff")
	}
	var width, height int
	if size, ok := payload["size"].(string); ok {
		widthText, heightText, _ := strings.Cut(size, "x")
		width, _ = strconv.Atoi(widthText)
		height, _ = strconv.Atoi(heightText)
	}
	requestedSide := 0
	if resolution, ok := payload["resolution"].(string); ok {
		requestedSide = imageTierSides[resolution]
	} else if width > 0 && height > 0 {
		for _, side := range imageTierSides {
			if side >= max(width, height) && (requestedSide == 0 || side < requestedSide) {
				requestedSide = side
			}
		}
	}
	outputMegapixels := unboundedImageMegapixels
	switch {
	case width > 0 && height > 0:
		outputMegapixels = float64(width) * float64(height) / 1_000_000
	case requestedSide > 0:
		outputMegapixels = float64(requestedSide*requestedSide) / 1_000_000
	default:
		largestSide := 0
		for _, resolution := range endpoint.SupportedParameters["resolution"].Values {
			largestSide = max(largestSide, imageTierSides[resolution])
		}
		if largestSide > 0 {
			outputMegapixels = float64(largestSide*largestSide) / 1_000_000
		}
	}

	// unitRates are the variant rates of one billable unit: the highest of all
	// variants and of those naming no resolution, and the highest rate of the
	// smallest resolution variant not below the requested tier.
	type unitRates struct {
		price          ImagePrice
		highest, plain float64
		tierSide       int
		tierRate       float64
	}
	units := map[string]*unitRates{}
	for _, price := range endpoint.Pricing {
		if price.missingCost || math.IsNaN(price.CostUSD) || math.IsInf(price.CostUSD, 0) || price.CostUSD < 0 {
			return 0, errors.New("invalid OpenRouter image tariff")
		}
		key := price.Billable + "/" + price.Unit
		unit, seen := units[key]
		if !seen {
			unit = &unitRates{price: price}
			units[key] = unit
		}
		unit.highest = max(unit.highest, price.CostUSD)
		side := imageTierSides[strings.ToUpper(price.Variant[strings.LastIndex(price.Variant, "_")+1:])]
		switch {
		case side == 0:
			unit.plain = max(unit.plain, price.CostUSD)
		case side < requestedSide:
			// A variant below the requested tier never prices it.
		case unit.tierSide == 0 || side < unit.tierSide:
			unit.tierSide, unit.tierRate = side, price.CostUSD
		case side == unit.tierSide:
			unit.tierRate = max(unit.tierRate, price.CostUSD)
		}
	}
	cost := 0.0
	for _, unit := range units {
		rate := unit.highest
		if requestedSide > 0 && unit.tierSide > 0 {
			rate = max(unit.tierRate, unit.plain)
		}
		quantity := float64(count)
		switch unit.price.Billable {
		case "output_image", "input_text":
		case "input_image", "input_reference":
			quantity = float64(references)
		case "input_font":
			// Fonts are not accepted by this adapter, so this published optional tariff is unused.
			quantity = 0
		default:
			return 0, errors.New("unknown OpenRouter image billable unit")
		}
		switch unit.price.Unit {
		case "image":
		case "request":
			if quantity > 0 {
				quantity = 1
			}
		case "megapixel":
			quantity *= outputMegapixels
		case "token":
			if unit.price.Billable == "input_text" {
				prompt, _ := payload["prompt"].(string)
				quantity *= float64(len(prompt) + 1024) // Byte upper estimate plus protocol overhead, not a tokenizer claim.
			} else {
				quantity *= math.Ceil(outputMegapixels * 4096) // Reservation estimate, not an asserted token conversion.
			}
		default:
			return 0, errors.New("unknown OpenRouter image pricing unit")
		}
		cost += quantity * rate
	}
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return 0, errors.New("invalid OpenRouter image cost estimate")
	}
	return cost, nil
}
