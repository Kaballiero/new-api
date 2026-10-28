package fetch_custom_oauth_discovery

import (
	"context"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/internal/identity/domain"
)

type Client interface {
	Fetch(context.Context, string) (map[string]any, error)
}
type Usecase struct{ client Client }

func NewUsecase(client Client) Usecase { return Usecase{client: client} }
func (u Usecase) Execute(ctx context.Context, req FetchCustomOAuthDiscoveryRequest) (Response, error) {
	wellKnownURL := strings.TrimSpace(req.WellKnownURL)
	issuerURL := strings.TrimSpace(req.IssuerURL)
	if wellKnownURL == "" && issuerURL == "" {
		return Response{}, domain.ErrCustomOAuthDiscoveryMissingURL
	}
	targetURL := wellKnownURL
	if targetURL == "" {
		targetURL = strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"
	}
	targetURL = strings.TrimSpace(targetURL)
	parsedURL, err := url.Parse(targetURL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return Response{}, domain.ErrCustomOAuthDiscoveryInvalidURL
	}
	discovery, err := u.client.Fetch(ctx, targetURL)
	if err != nil {
		return Response{}, err
	}
	return Response{WellKnownURL: targetURL, Discovery: discovery}, nil
}
