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
// An explicit WxH size has the tier whose side is the smallest covering its
// longer side (the longer side itself above every tier) and the output W×H. A
// resolution tier counts as its side squared; with a size the larger tier and
// output win. A request without either reserves the largest tier the endpoint
// publishes. A size the endpoint does not declare never lowers the reserve:
// it only raises the output, and the tier when a resolution names one. A tier
// name outside imageTierSides, requested or published, reserves
// unboundedImageMegapixels and no tier.
func requestedImageSize(endpoint ImageEndpoint, payload map[string]any) (int, float64) {
	sizeSide, sizeMegapixels := 0, 0.0
	size, _ := payload["size"].(string)
	widthText, heightText, _ := strings.Cut(size, "x")
	width, _ := strconv.Atoi(widthText)
	height, _ := strconv.Atoi(heightText)
	if width > 0 && height > 0 {
		longer := max(width, height)
		sizeSide, sizeMegapixels = longer, float64(width)*float64(height)/1_000_000
		for _, tierSide := range slices.Sorted(maps.Values(imageTierSides)) {
			if tierSide >= longer {
				sizeSide = tierSide
				break
			}
		}
	}
	_, sized := endpoint.SupportedParameters["size"]
	side, megapixels := 0, 0.0
	if sized {
		side, megapixels = sizeSide, sizeMegapixels
	}
	resolution, requested := payload["resolution"].(string)
	resolutionSide, known := imageTierSides[resolution]
	switch {
	case requested && !known:
		return 0, max(sizeMegapixels, unboundedImageMegapixels)
	case requested:
		side = max(side, resolutionSide)
		megapixels = max(megapixels, float64(resolutionSide*resolutionSide)/1_000_000)
	case megapixels == 0:
		megapixels = publishedImageMegapixels(endpoint)
	}
	if !sized && side > 0 {
		side = max(side, sizeSide)
	}
	return side, max(megapixels, sizeMegapixels)
}

// publishedImageMegapixels is the output of the largest tier an endpoint
// publishes; unboundedImageMegapixels when it publishes none or a tier name
// outside imageTierSides.
func publishedImageMegapixels(endpoint ImageEndpoint) float64 {
	largestSide := 0
	for _, resolution := range endpoint.SupportedParameters["resolution"].Values {
		resolutionSide, known := imageTierSides[resolution]
		if !known {
			return unboundedImageMegapixels
		}
		largestSide = max(largestSide, resolutionSide)
	}
	if largestSide == 0 {
		return unboundedImageMegapixels
	}
	return float64(largestSide*largestSide) / 1_000_000
}
