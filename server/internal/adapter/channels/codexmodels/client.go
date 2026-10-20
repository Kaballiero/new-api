package codexmodels

import (
	"context"
	"github.com/QuantumNous/new-api/internal/channels/discover_codex_models"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"net/http"
)

type Version func(context.Context, *http.Client) (string, error)
type Models func(context.Context, *http.Client, string, *domain.CodexOAuthKey, string) (int, []string, error)
type Remote struct {
	open    func(string) (*http.Client, error)
	version Version
	models  Models
}

func NewRemote(open func(string) (*http.Client, error), version Version, models Models) Remote {
	return Remote{open: open, version: version, models: models}
}
func (remote Remote) Open(proxy string) (discover_codex_models.Session, error) {
	client, err := remote.open(proxy)
	if err != nil {
		return nil, err
	}
	return NewSession(client, remote.version, remote.models), nil
}

type Session struct {
	client  *http.Client
	version Version
	models  Models
}

func NewSession(client *http.Client, version Version, models Models) Session {
	return Session{client: client, version: version, models: models}
}
func (session Session) Version(ctx context.Context) (string, error) {
	return session.version(ctx, session.client)
}
func (session Session) Models(ctx context.Context, base string, key *domain.CodexOAuthKey, version string) (int, []string, error) {
	return session.models(ctx, session.client, base, key, version)
}
