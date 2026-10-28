package update_option

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/internal/settings/domain"
)

type Validation interface {
	RequirementSatisfied(string) bool
	Check(string, string) error
	ComplianceConfirmed() bool
	TaskPublicAddress(string) error
}
type Writer interface{ Execute(string, string) error }
type Usecase struct {
	validation Validation
	writer     Writer
}

func NewUsecase(validation Validation, writer Writer) Usecase { return Usecase{validation, writer} }
func (u Usecase) Execute(key, value string) error {
	switch key {
	case "QuotaForInviter", "QuotaForInvitee":
		positive := false
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			positive = n > 0
		} else if n, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
			positive = n > 0
		}
		if positive && !u.validation.ComplianceConfirmed() {
			return domain.ErrComplianceRequired
		}
	default:
		if strings.HasPrefix(key, "payment_setting.compliance_") {
			return domain.ErrComplianceField
		}
	}
	if key == "TaskPublicAddress" && value != "" {
		if err := u.validation.TaskPublicAddress(value); err != nil {
			return err
		}
	}
	switch key {
	case "GitHubOAuthEnabled", "discord.enabled", "oidc.enabled", "LinuxDOOAuthEnabled", "EmailDomainRestrictionEnabled",
		"WeChatAuthEnabled", "TurnstileCheckEnabled", "TelegramOAuthEnabled":
		if value == "true" && !u.validation.RequirementSatisfied(key) {
			return &domain.InvalidOption{Kind: key}
		}
	case "theme.frontend":
		if value != "default" {
			return domain.ErrRetiredTheme
		}
	default:
		if err := u.validation.Check(key, value); err != nil {
			return err
		}
	}
	return u.writer.Execute(key, value)
}
