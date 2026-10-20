package service

import (
	"fmt"
	oauthadapter "github.com/QuantumNous/new-api/internal/adapter/identity/oauthunlink"
	"github.com/QuantumNous/new-api/internal/identity/unlink_oauth_account"
	"html"

	"github.com/QuantumNous/new-api/common"
)

func UnbindAccountOAuth(identity AuthIdentity, providerID int) error {
	return unlink_oauth_account.ExecuteMutation(oauthadapter.UnlinkMutation{Identity: identity, ProviderID: providerID})
}

// NotifyAccountSecurityChange never includes credentials or tokens. The caller
// records delivery failure independently from the already-committed change.
func NotifyAccountSecurityChange(email, event string) error {
	if email == "" {
		return nil
	}
	subject := common.SystemName + " — Account security notification"
	content := fmt.Sprintf("<p>Your account security settings have changed: %s.</p><p>If you did not make this change, open your account security settings, revoke other login sessions, and contact your administrator.</p>", html.EscapeString(event))
	return common.SendEmail(subject, email, content)
}
