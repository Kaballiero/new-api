package catalogsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const MaxDocumentBytes = 8 << 20
const maxManifestBytes = 4 << 20
const requestTimeout = 30 * time.Second

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NormalizeBaseURL(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Host == "" || !((parsed.Scheme == "https") || (parsed.Scheme == "http")) || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || len(baseURL) > 2048 {
		return "", fmt.Errorf("invalid catalog API base URL")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	normalized, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		return nil, fmt.Errorf("catalog HTTP client unavailable")
	}
	client := *httpClient
	// Catalog redirects cannot change the configured trust boundary.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: normalized, httpClient: &client}, nil
}
func (c *Client) BaseURL() string { return c.baseURL }
func (c *Client) read(ctx context.Context, path string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog HTTP request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog HTTP status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("catalog response exceeds size limit")
	}
	return raw, nil
}
func (c *Client) Manifest(ctx context.Context) (Manifest, error) {
	raw, err := c.read(ctx, "/v1/catalog/manifest", maxManifestBytes)
	if err != nil {
		return Manifest{}, err
	}
	object, err := wireObject(raw, nil, "schemaVersion", "normalizationVersion", "revision", "sourceRevision", "fetchedAt", "models")
	if err != nil {
		return Manifest{}, err
	}
	var refs []json.RawMessage
	if err := common.Unmarshal(object["models"], &refs); err != nil {
		return Manifest{}, err
	}
	for _, ref := range refs {
		if _, err := wireObject(ref, nil, "id", "hash"); err != nil {
			return Manifest{}, err
		}
	}
	var manifest Manifest
	if err := common.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("invalid catalog manifest JSON: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
func (c *Client) Document(ctx context.Context, ref Reference) ([]byte, *Document, error) {
	if ref.ID == "" || !ValidHash(ref.Hash) {
		return nil, nil, fmt.Errorf("invalid catalog document reference")
	}
	raw, err := c.read(ctx, "/v1/catalog/documents/"+ref.Hash, MaxDocumentBytes)
	if err != nil {
		return nil, nil, err
	}
	doc, err := DecodeDocument(raw, ref)
	if err != nil {
		return nil, nil, err
	}
	return raw, doc, nil
}
