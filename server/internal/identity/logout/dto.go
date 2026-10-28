package logout

type Input struct {
	ExpectedSID, RefreshToken, AccessToken string
	CookiePresent                          bool
}
type Result struct {
	RevokedSID  string
	ClearCookie bool
	Detailed    bool
}
