package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// ReadEndpointRate reads one OpenRouter endpoint price, a USD amount per
// token, request or image published as a JSON string or number. An absent or
// null price is ErrEndpointPriceMissing.
func ReadEndpointRate(deps Capabilities, raw json.RawMessage) (decimal.Decimal, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return decimal.Zero, ErrEndpointPriceMissing
	}
	if len(value) > 1 && value[0] == '"' {
		if deps.Codec.Unmarshal(raw, &value) != nil {
			return decimal.Zero, errors.New("invalid OpenRouter endpoint price")
		}
	}
	finite, parseErr := strconv.ParseFloat(value, 64)
	if parseErr != nil || math.IsInf(finite, 0) || math.IsNaN(finite) || finite < 0 {
		return decimal.Zero, errors.New("invalid OpenRouter endpoint price")
	}
	rate, err := decimal.NewFromString(value)
	if err != nil || rate.IsNegative() || rate.Exponent() < -324 || rate.Exponent() > 308 {
		return decimal.Zero, errors.New("missing or invalid OpenRouter endpoint price")
	}
	return rate, nil
}

// ReadEndpointPrice reads one OpenRouter endpoint price for display; a
// positive price that rounds to zero is rejected.
func ReadEndpointPrice(deps Capabilities, raw json.RawMessage) (float64, error) {
	rate, err := ReadEndpointRate(deps, raw)
	if err != nil {
		return 0, err
	}
	amount, _ := rate.Float64()
	if amount == 0 && !rate.IsZero() {
		return 0, errors.New("native endpoint price underflows display precision")
	}
	return amount, nil
}
