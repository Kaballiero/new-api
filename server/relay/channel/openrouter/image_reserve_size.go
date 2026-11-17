package openrouter

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// imageTierSides are the sides in pixels of the resolution tiers a request
// names. Tariff variants name them in lower case, alone ("1k") or as the last
// part of a variant ("low_1k").
var imageTierSides = map[string]int{"512": 512, "768": 768, "1K": 1024, "1.5K": 1536, "2K": 2048, "4K": 4096}

// unboundedImageMegapixels sizes an image whose size the reserve cannot
// bound: a reference image, or an output of a model that publishes no tier
// it knows when the request names none.
const unboundedImageMegapixels = 16.0

// requestedImageSize is the output an image request reserves: the side of its
// requested tier, zero when it names none, and its megapixels.
//
// An explicit WxH size counts only on an endpoint that declares size: its tier
// is the smallest whose side covers the longer side (the longer side itself
// above every tier) and its output W×H. A resolution tier counts as its side
// squared; with a size the larger tier and output win. A request without
// either reserves the largest tier the endpoint publishes. A tier name outside
// imageTierSides, requested or published, reserves unboundedImageMegapixels
// and no tier.
func requestedImageSize(endpoint ImageEndpoint, payload map[string]any) (int, float64) {
	side, megapixels := 0, 0.0
	size, _ := payload["size"].(string)
	widthText, heightText, _ := strings.Cut(size, "x")
	width, _ := strconv.Atoi(widthText)
	height, _ := strconv.Atoi(heightText)
	if _, sized := endpoint.SupportedParameters["size"]; sized && width > 0 && height > 0 {
		longer := max(width, height)
		side, megapixels = longer, float64(width)*float64(height)/1_000_000
		for _, tierSide := range slices.Sorted(maps.Values(imageTierSides)) {
			if tierSide >= longer {
				side = tierSide
				break
			}
		}
	}
	if resolution, ok := payload["resolution"].(string); ok {
		resolutionSide, known := imageTierSides[resolution]
		if !known {
			return 0, max(megapixels, unboundedImageMegapixels)
		}
		side = max(side, resolutionSide)
		megapixels = max(megapixels, float64(resolutionSide*resolutionSide)/1_000_000)
	}
	if megapixels > 0 {
		return side, megapixels
	}
	largestSide := 0
	for _, resolution := range endpoint.SupportedParameters["resolution"].Values {
		resolutionSide, known := imageTierSides[resolution]
		if !known {
			return 0, unboundedImageMegapixels
		}
		largestSide = max(largestSide, resolutionSide)
	}
	if largestSide == 0 {
		return 0, unboundedImageMegapixels
	}
	return 0, float64(largestSide*largestSide) / 1_000_000
}
