package openrouter

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

// Speech duration of a text at speed 1, an approximation by the owner rule
// of 14.11: the longer of two words a second and five characters a second.
const (
	speechWordsPerSecond      = 2
	speechCharactersPerSecond = 5
)

// The speed multiplier OpenAI TTS accepts; OpenRouter passes speed to the
// provider.
const (
	speechMinSpeed = 0.25
	speechMaxSpeed = 4
)

// speechUnits measures a speech request in the units a TTS endpoint prices
// per request: the characters and UTF-8 bytes of the input exactly, its
// seconds by the owner rule, and its input tokens as the tokens of the input
// plus those of the instructions, an upper bound. Slower speech lengthens the
// seconds; faster speech, which a provider may ignore, does not shorten them.
// The request body is at most the pricing inspection limit, so every count
// fits int64.
func speechUnits(body []byte, model string) (map[pricingdomain.PriceDimension]int64, error) {
	var request struct {
		Input        string   `json:"input"`
		Instructions string   `json:"instructions"`
		Speed        *float64 `json:"speed"`
	}
	if common.Unmarshal(body, &request) != nil {
		return nil, errors.New("invalid OpenRouter speech request")
	}
	speed := 1.0
	if request.Speed != nil {
		speed = *request.Speed
	}
	if speed < speechMinSpeed || speed > speechMaxSpeed {
		return nil, errors.New("OpenRouter speech speed must be between 0.25 and 4")
	}
	characters := utf8.RuneCountInString(request.Input)
	words := len(strings.Fields(request.Input))
	seconds := max((words+speechWordsPerSecond-1)/speechWordsPerSecond, (characters+speechCharactersPerSecond-1)/speechCharactersPerSecond)
	inputTokens := tokenusage.CountTextToken(request.Input, model) + tokenusage.CountTextToken(request.Instructions, model)
	return map[pricingdomain.PriceDimension]int64{
		{Billable: pricingdomain.PriceBillableOutputSpeech, Unit: pricingdomain.PriceUnitCharacter}:  int64(characters),
		{Billable: pricingdomain.PriceBillableOutputSpeech, Unit: pricingdomain.PriceUnitUTF8Byte}:   int64(len(request.Input)),
		{Billable: pricingdomain.PriceBillableOutputAudio, Unit: pricingdomain.PriceUnitSecond}:      int64(math.Ceil(float64(seconds) / min(speed, 1))),
		{Billable: pricingdomain.PriceBillableInputText, Unit: pricingdomain.PriceUnitMillionTokens}: int64(inputTokens),
	}, nil
}
