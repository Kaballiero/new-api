// Package service preserves the legacy wire type namespace in JSON diagnostics.
// Its implementation belongs to the pricing FX HTTP adapter.
package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	refresh "github.com/QuantumNous/new-api/internal/pricing/refresh_cost_fx"
	"io"
	"net/http"
	"time"
)

const (
	Timeout                = 10 * time.Second
	modelCostFXMaxBodySize = 1 << 20
)

type Client struct{ Endpoint func() string }

func (c Client) Fetch(ctx context.Context) (refresh.FXProviderResponse, error) {
	requestCtx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, c.Endpoint(), nil)
	if err != nil {
		return refresh.FXProviderResponse{}, err
	}
	client := &http.Client{Timeout: Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		return refresh.FXProviderResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return refresh.FXProviderResponse{}, fmt.Errorf("model cost FX provider returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, modelCostFXMaxBodySize+1))
	if err != nil {
		return refresh.FXProviderResponse{}, err
	}
	if len(body) > modelCostFXMaxBodySize {
		return refresh.FXProviderResponse{}, errors.New("model cost FX provider response is too large")
	}
	var provider modelCostFXProviderResponse
	if err := common.Unmarshal(body, &provider); err != nil {
		return refresh.FXProviderResponse{}, fmt.Errorf("decode model cost FX provider response: %w", err)
	}
	result := refresh.FXProviderResponse{Date: provider.Date}
	if provider.Valute != nil {
		result.Valute = make(map[string]refresh.Currency, len(provider.Valute))
		for key, value := range provider.Valute {
			result.Valute[key] = refresh.Currency{CharCode: value.CharCode, Value: value.Value, Nominal: value.Nominal}
		}
	}
	return result, nil
}

type modelCostFXProviderResponse struct {
	Date   string `json:"Date"`
	Valute map[string]struct {
		CharCode string  `json:"CharCode"`
		Value    float64 `json:"Value"`
		Nominal  float64 `json:"Nominal"`
	} `json:"Valute"`
}
