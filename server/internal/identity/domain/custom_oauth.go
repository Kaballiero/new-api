package domain

import (
	"errors"
	"time"
)

type CustomOAuthProvider struct {
	Id                    int
	Name                  string
	Slug                  string
	Icon                  string
	Enabled               bool
	ClientId              string
	AuthorizationEndpoint string
	TokenEndpoint         string
	UserInfoEndpoint      string
	Scopes                string
	UserIdField           string
	UsernameField         string
	DisplayNameField      string
	EmailField            string
	WellKnown             string
	AuthStyle             int
	AccessPolicy          string
	AccessDeniedMessage   string
	ClientSecret          string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

var (
	ErrCustomOAuthProviderMissing     = errors.New("custom OAuth provider missing")
	ErrCustomOAuthSlugTaken           = errors.New("custom OAuth slug taken")
	ErrCustomOAuthBuiltinConflict     = errors.New("custom OAuth built-in slug conflict")
	ErrCustomOAuthBindingCheck        = errors.New("custom OAuth binding check failed")
	ErrCustomOAuthProviderBound       = errors.New("custom OAuth provider has bindings")
	ErrCustomOAuthDiscoveryMissingURL = errors.New("custom OAuth discovery URL required")
	ErrCustomOAuthDiscoveryInvalidURL = errors.New("custom OAuth discovery URL invalid")
)

// DiscoveryError identifies a failed stage without coupling the client to HTTP.
type DiscoveryError struct {
	Stage string
	Cause error
}

func (e *DiscoveryError) Error() string { return e.Cause.Error() }
func (e *DiscoveryError) Unwrap() error { return e.Cause }
