package bind_verification_operation

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/internal/identity/domain"
	"strings"
)

type Codec interface {
	Object(json.RawMessage) bool
	Decode([]byte, any) error
	NormalizeEmail(string) string
	EmailValid(string) bool
	ProviderExists(string) bool
	Digest(scope string, context any) (string, error)
}

func (usecase Usecase) Execute(operation domain.VerificationOperation) (domain.VerificationBinding, error) {
	var fields map[string]json.RawMessage
	if len(operation.Context) > 0 {
		if !usecase.codec.Object(operation.Context) || usecase.codec.Decode(operation.Context, &fields) != nil {
			return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
		}
	}
	var normalized any
	switch operation.Scope {
	case domain.VerificationScopeChannelKeyRead:
		var context domain.ChannelKeyReadContext
		if len(fields) != 1 || usecase.codec.Decode(fields["channel_id"], &context.ChannelID) != nil || context.ChannelID <= 0 {
			return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
		}
		normalized = context
	case domain.VerificationScopeAccountBind:
		var context domain.AccountBindingContext
		if usecase.codec.Decode(operation.Context, &context) != nil {
			return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
		}
		context.Provider = strings.TrimSpace(context.Provider)
		switch context.Provider {
		case "email":
			context.Email = usecase.codec.NormalizeEmail(context.Email)
			if len(fields) != 2 || !usecase.codec.EmailValid(context.Email) || context.Code != "" {
				return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
			}
		case "wechat":
			context.Code = strings.TrimSpace(context.Code)
			if len(fields) != 2 || context.Code == "" || len(context.Code) > 128 || context.Email != "" {
				return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
			}
		default:
			if len(fields) != 1 || context.Provider == "" || len(context.Provider) > 64 || !usecase.codec.ProviderExists(context.Provider) {
				return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
			}
		}
		normalized = context
	case domain.VerificationScopeAccountUnbind:
		var context domain.AccountUnbindingContext
		if len(fields) != 1 || usecase.codec.Decode(fields["provider_id"], &context.ProviderID) != nil || context.ProviderID <= 0 {
			return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
		}
		normalized = context
	case domain.VerificationScopePasskeyRegister, domain.VerificationScopePasskeyDelete, domain.VerificationScopeTwoFASetup,
		domain.VerificationScopeTwoFADisable, domain.VerificationScopeTwoFABackupCodes,
		domain.VerificationScopeAccessTokenGenerate, domain.VerificationScopeAccessTokenRevoke,
		domain.VerificationScopePasswordSet, domain.VerificationScopePasswordChange, domain.VerificationScopeAccountDelete:
		if len(fields) != 0 {
			return domain.VerificationBinding{}, domain.ErrVerificationContextInvalid
		}
		normalized = struct{}{}
	default:
		return domain.VerificationBinding{}, domain.ErrProofScope
	}
	hash, err := usecase.codec.Digest(operation.Scope, normalized)
	if err != nil {
		return domain.VerificationBinding{}, err
	}
	return domain.VerificationBinding{Scope: operation.Scope, ContextHash: hash}, nil
}

type Usecase struct {
	codec Codec
}

func NewUsecase(codec Codec) Usecase {
	return Usecase{codec: codec}
}
