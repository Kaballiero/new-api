package identity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Discovery struct{}

func (Discovery) Fetch(parent context.Context, targetURL string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, &domain.DiscoveryError{Stage: "request", Cause: err}
	}
	httpReq.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, &domain.DiscoveryError{Stage: "fetch", Cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, &domain.DiscoveryError{Stage: "fetch", Cause: errors.New(message)}
	}
	var discovery map[string]any
	if err = common.DecodeJson(resp.Body, &discovery); err != nil {
		return nil, &domain.DiscoveryError{Stage: "decode", Cause: err}
	}
	return discovery, nil
}
