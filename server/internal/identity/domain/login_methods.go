package domain

type LoginMethods struct {
	Password, Passkey, WeChat bool
	OAuthColumns              []string
	CustomProviderIDs         []int
}

type LoginCredentials struct {
	Password, WeChatID string
	OAuthColumns       map[string]string
}
type AlternativeLoginReader interface {
	Passkeys() (int64, error)
	OtherOAuthBindings(providerIDs []int) (int64, error)
}

// HasAlternativeLogin evaluates the locked credential snapshot. Counts are read
// only when the earlier mechanisms did not establish another usable login.
func HasAlternativeLogin(enabled LoginMethods, current LoginCredentials, reader AlternativeLoginReader) (bool, error) {
	hasLogin := enabled.Password && current.Password != "" || enabled.WeChat && current.WeChatID != ""
	for _, column := range enabled.OAuthColumns {
		hasLogin = hasLogin || current.OAuthColumns[column] != ""
	}
	if !hasLogin && enabled.Passkey {
		count, err := reader.Passkeys()
		if err != nil {
			return false, err
		}
		hasLogin = count > 0
	}
	if !hasLogin && len(enabled.CustomProviderIDs) > 0 {
		count, err := reader.OtherOAuthBindings(enabled.CustomProviderIDs)
		if err != nil {
			return false, err
		}
		hasLogin = count > 0
	}
	return hasLogin, nil
}
