package openrouter

import (
	"errors"
	"math"
	"strings"
)

// EstimateImageCost reserves an estimate of an image request from the tariffs
// of its endpoint; it is never substituted for actual usage.cost, which
// reconciles it.
//
// The output (output_image) is priced for the requested size
// (requestedImageSize). Without a requested tier it takes the highest
// variant. With one, it takes the smallest published resolution variant not
// below the tier, or the highest variant when none is, and the variants that
// name no resolution ("high_resolution", "style_references", none) when they
// are higher. Inputs keep the highest variant and size every reference at
// unboundedImageMegapixels. A megapixel is priced in whole megapixels, at
// least one per image; a token as 4096 per megapixel.
func EstimateImageCost(endpoint ImageEndpoint, count, references int, payload map[string]any) (float64, error) {
	if len(endpoint.Pricing) == 0 {
		return 0, errors.New("OpenRouter image endpoint has no published tariff")
	}
	requestedSide, outputMegapixels := requestedImageSize(endpoint, payload)

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
		output := unit.price.Billable == "output_image"
		rate, megapixels := unit.highest, unboundedImageMegapixels
		if output {
			megapixels = outputMegapixels
		}
		if output && requestedSide > 0 && unit.tierSide > 0 {
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
			quantity *= max(1, math.Ceil(megapixels))
		case "token":
			if unit.price.Billable == "input_text" {
				prompt, _ := payload["prompt"].(string)
				quantity *= float64(len(prompt) + 1024) // Byte upper estimate plus protocol overhead, not a tokenizer claim.
			} else {
				quantity *= math.Ceil(megapixels * 4096) // Reservation estimate, not an asserted token conversion.
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
