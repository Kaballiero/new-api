package identitycore

import (
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauthunlink"
	identitystorage "github.com/QuantumNous/new-api/internal/app/identitystorage"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"github.com/QuantumNous/new-api/internal/identity/unlink_oauth_account"
)

func UnbindAccountOAuth(identity domain.AuthIdentity, providerID int) error {
	return unlink_oauth_account.ExecuteMutation(oauthadapter.UnlinkMutation{SQL: identitystorage.SQL(), Identity: identity, ProviderID: providerID})
}
