package identity

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	identityrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/identity/record"
)

type accessPolicyPayload struct {
	Logic      string                `json:"logic"`
	Conditions []accessConditionItem `json:"conditions"`
	Groups     []accessPolicyPayload `json:"groups"`
}

type accessConditionItem struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

var supportedAccessPolicyOps = map[string]struct{}{
	"eq":           {},
	"ne":           {},
	"gt":           {},
	"gte":          {},
	"lt":           {},
	"lte":          {},
	"in":           {},
	"not_in":       {},
	"contains":     {},
	"not_contains": {},
	"exists":       {},
	"not_exists":   {},
}

// GetAllCustomOAuthProviders returns all custom OAuth providers
func (s Store) GetAllCustomOAuthProviders() ([]*identityrecord.CustomOAuthProvider, error) {
	var providers []*identityrecord.CustomOAuthProvider
	err := s.env.DB().Order("id asc").Find(&providers).Error
	return providers, err
}

// GetCustomOAuthProviderById returns a custom OAuth provider by ID
func (s Store) GetCustomOAuthProviderById(id int) (*identityrecord.CustomOAuthProvider, error) {
	var provider identityrecord.CustomOAuthProvider
	err := s.env.DB().First(&provider, id).Error
	if err != nil {
		return nil, err
	}
	return &provider, nil
}

// CreateCustomOAuthProvider creates a new custom OAuth provider
func (s Store) CreateCustomOAuthProvider(provider *identityrecord.CustomOAuthProvider) error {
	if err := s.validateCustomOAuthProvider(provider); err != nil {
		return err
	}
	return s.env.DB().Create(provider).Error
}

// UpdateCustomOAuthProvider updates an existing custom OAuth provider
func (s Store) UpdateCustomOAuthProvider(provider *identityrecord.CustomOAuthProvider) error {
	if err := s.validateCustomOAuthProvider(provider); err != nil {
		return err
	}
	return s.env.DB().Save(provider).Error
}

// DeleteCustomOAuthProvider deletes a custom OAuth provider by ID
func (s Store) DeleteCustomOAuthProvider(id int) error {
	// First, delete all user bindings for this provider
	if err := s.env.DB().Where("provider_id = ?", id).Delete(&identityrecord.UserOAuthBinding{}).Error; err != nil {
		return err
	}
	return s.env.DB().Delete(&identityrecord.CustomOAuthProvider{}, id).Error
}

// IsSlugTaken checks if a slug is already taken by another provider
// Returns true on DB errors (fail-closed) to prevent slug conflicts
func (s Store) IsSlugTaken(slug string, excludeId int) bool {
	var count int64
	query := s.env.DB().Model(&identityrecord.CustomOAuthProvider{}).Where("slug = ?", slug)
	if excludeId > 0 {
		query = query.Where("id != ?", excludeId)
	}
	res := query.Count(&count)
	if res.Error != nil {
		// Fail-closed: treat DB errors as slug being taken to prevent conflicts
		return true
	}
	return count > 0
}

// validateCustomOAuthProvider validates a custom OAuth provider configuration
func (s Store) validateCustomOAuthProvider(provider *identityrecord.CustomOAuthProvider) error {
	if provider.Name == "" {
		return errors.New("provider name is required")
	}
	if provider.Slug == "" {
		return errors.New("provider slug is required")
	}
	// Slug must be lowercase and contain only alphanumeric characters and hyphens
	slug := strings.ToLower(provider.Slug)
	for _, c := range slug {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return errors.New("provider slug must contain only lowercase letters, numbers, and hyphens")
		}
	}
	provider.Slug = slug
	if slug == "telegram" {
		return errors.New("the telegram slug is reserved for built-in Telegram OAuth; rename this custom provider")
	}

	if provider.ClientId == "" {
		return errors.New("client ID is required")
	}
	if provider.AuthorizationEndpoint == "" {
		return errors.New("authorization endpoint is required")
	}
	if provider.TokenEndpoint == "" {
		return errors.New("token endpoint is required")
	}
	if provider.UserInfoEndpoint == "" {
		return errors.New("user info endpoint is required")
	}

	// Set defaults for field mappings if empty
	if provider.UserIdField == "" {
		provider.UserIdField = "sub"
	}
	if provider.UsernameField == "" {
		provider.UsernameField = "preferred_username"
	}
	if provider.DisplayNameField == "" {
		provider.DisplayNameField = "name"
	}
	if provider.EmailField == "" {
		provider.EmailField = "email"
	}
	if provider.Scopes == "" {
		provider.Scopes = "openid profile email"
	}
	if strings.TrimSpace(provider.AccessPolicy) != "" {
		var policy accessPolicyPayload
		if err := common.UnmarshalJsonStr(provider.AccessPolicy, &policy); err != nil {
			return errors.New("access_policy must be valid JSON")
		}
		if err := s.validateAccessPolicyPayload(&policy); err != nil {
			return fmt.Errorf("access_policy is invalid: %w", err)
		}
	}

	return nil
}

func (s Store) validateAccessPolicyPayload(policy *accessPolicyPayload) error {
	if policy == nil {
		return errors.New("policy is nil")
	}

	logic := strings.ToLower(strings.TrimSpace(policy.Logic))
	if logic == "" {
		logic = "and"
	}
	if logic != "and" && logic != "or" {
		return fmt.Errorf("unsupported logic: %s", logic)
	}

	if len(policy.Conditions) == 0 && len(policy.Groups) == 0 {
		return errors.New("policy requires at least one condition or group")
	}

	for index, condition := range policy.Conditions {
		field := strings.TrimSpace(condition.Field)
		if field == "" {
			return fmt.Errorf("condition[%d].field is required", index)
		}
		op := strings.ToLower(strings.TrimSpace(condition.Op))
		if _, ok := supportedAccessPolicyOps[op]; !ok {
			return fmt.Errorf("condition[%d].op is unsupported: %s", index, op)
		}
		if op == "in" || op == "not_in" {
			if _, ok := condition.Value.([]any); !ok {
				return fmt.Errorf("condition[%d].value must be an array for op %s", index, op)
			}
		}
	}

	for index := range policy.Groups {
		if err := s.validateAccessPolicyPayload(&policy.Groups[index]); err != nil {
			return fmt.Errorf("group[%d]: %w", index, err)
		}
	}

	return nil
}
