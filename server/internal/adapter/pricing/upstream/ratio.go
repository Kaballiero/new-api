package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/fetch_upstream_ratios"
)

type RatioClients struct {
	HTTP func() *http.Client
	Key  func(int) (string, error)
	Warn func(context.Context, string, ...any)
}

func (r RatioClients) NewClient() fetch_upstream_ratios.Client {
	return ratioClient{http: r.HTTP(), key: r.Key, warn: r.Warn}
}

type ratioClient struct {
	http *http.Client
	key  func(int) (string, error)
	warn func(context.Context, string, ...any)
}

func (r ratioClient) Fetch(ctx context.Context, url string, openrouter bool, id int, name string) ([]byte, func(), error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		r.warn(ctx, "build request failed: "+err.Error())
		return nil, nil, err
	}
	if openrouter && id != 0 {
		key, err := r.key(id)
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(key) == "" {
			return nil, nil, fmt.Errorf("no API key configured for this channel")
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(key))
	} else if openrouter {
		return nil, nil, fmt.Errorf("OpenRouter requires a valid channel with API key")
	}
	var response *http.Response
	var lastErr error
	for attempt := range 3 {
		response, lastErr = r.http.Do(request)
		if lastErr == nil {
			break
		}
		time.Sleep(time.Duration(200*(1<<attempt)) * time.Millisecond)
	}
	if lastErr != nil {
		r.warn(ctx, "http error on "+name+": "+lastErr.Error())
		return nil, nil, lastErr
	}
	closeBody := func() { _ = response.Body.Close() }
	if response.StatusCode != http.StatusOK {
		r.warn(ctx, "non-200 from "+name+": "+response.Status)
		return nil, closeBody, fmt.Errorf("%s", response.Status)
	}
	if ct := response.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "application/json") {
		r.warn(ctx, "unexpected content-type from "+name+": "+ct)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 10<<20))
	if err != nil {
		r.warn(ctx, "read response failed from "+name+": "+err.Error())
		return nil, closeBody, err
	}
	return body, closeBody, nil
}
