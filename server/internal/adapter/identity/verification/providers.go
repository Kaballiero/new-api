package verification

import (
	"sort"

	"github.com/QuantumNous/new-api/internal/identity/get_verification_requirements"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
)

func OAuthProviders(user *model.User) ([]get_verification_requirements.VerificationOAuthProvider, error) {
	bindings := map[int]string{}
	if len(oauth.GetEnabledCustomProviders()) > 0 {
		stored, err := model.GetUserOAuthBindingsByUserId(user.Id)
		if err != nil {
			return nil, err
		}
		for _, binding := range stored {
			bindings[binding.ProviderId] = binding.ProviderUserId
		}
	}
	providers := []get_verification_requirements.VerificationOAuthProvider{}
	for slug, provider := range oauth.GetAllProviders() {
		if !provider.IsEnabled() {
			continue
		}
		var userID string
		if custom, ok := provider.(*oauth.GenericOAuthProvider); ok {
			userID = bindings[custom.GetProviderId()]
		} else {
			switch provider.ProviderUserIDColumn() {
			case "github_id":
				userID = user.GitHubId
			case "discord_id":
				userID = user.DiscordId
			case "oidc_id":
				userID = user.OidcId
			case "linux_do_id":
				userID = user.LinuxDOId
			case "telegram_id":
				userID = user.TelegramId
			}
		}
		if userID != "" {
			providers = append(providers, get_verification_requirements.VerificationOAuthProvider{Slug: slug, Name: provider.GetName(), UserID: userID})
		}
	}
	sort.Slice(providers, func(i, j int) bool { return providers[i].Slug < providers[j].Slug })
	return providers, nil
}
