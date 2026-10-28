package provision_account

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/accounts/domain"
)

const maxProvisionedUserGroupRunes = 64

type ProvisionOperation interface {
	WithAccount(account NewAccount, provision func(ProvisionAccount) error) error
}

type ProvisionAccount interface {
	EnsureUsername() error
	SetCredential(string, int64)
	SetSidebar(string)
	Insert() (userID int, role int, err error)
	SetInitialQuota(quota int) error
	Quota() int
}

type Credentials interface{ NewProvisionedPAT() (string, int64, error) }
type Usecase struct {
	operation                           ProvisionOperation
	credentials                         Credentials
	usernameConflict                    func(error) bool
	groupExists                         func(string) bool
	sidebar                             func(int) string
	maxQuota, commonRole, enabledStatus int
}

func NewUsecase(operation ProvisionOperation, credentials Credentials, usernameConflict func(error) bool, groupExists func(string) bool, sidebar func(int) string, maxQuota, commonRole, enabledStatus int) Usecase {
	return Usecase{operation: operation, credentials: credentials, usernameConflict: usernameConflict, groupExists: groupExists, sidebar: sidebar, maxQuota: maxQuota, commonRole: commonRole, enabledStatus: enabledStatus}
}

func (u Usecase) Execute(principalRole int, request GetAPICreateUserRequest) (*GetAPICreateCredential, error) {
	if request.InitialQuota != nil && (*request.InitialQuota < 0 || *request.InitialQuota > u.maxQuota) {
		return nil, domain.ErrGetAPIInvalidRequest
	}
	if utf8.RuneCountInString(request.Group) > maxProvisionedUserGroupRunes || (request.Group != "" && !u.groupExists(request.Group)) {
		return nil, domain.ErrGetAPIInvalidRequest
	}
	var credential *GetAPICreateCredential
	accountInput := NewAccount{Username: strings.TrimSpace(request.Username), Password: request.Password, DisplayName: request.DisplayName, Group: request.Group, Role: u.commonRole, Status: u.enabledStatus}
	if accountInput.DisplayName == "" {
		accountInput.DisplayName = accountInput.Username
	}
	err := u.operation.WithAccount(accountInput, func(account ProvisionAccount) error {
		if err := account.EnsureUsername(); err != nil {
			if errors.Is(err, domain.ErrUsernameAlreadyTaken) {
				return domain.ErrGetAPICreateConflict
			}
			return domain.ErrGetAPICredentialUnavailable
		}
		token, now, err := u.credentials.NewProvisionedPAT()
		if err != nil {
			return domain.ErrGetAPICredentialUnavailable
		}
		account.SetCredential(token, now)
		account.SetSidebar(u.sidebar(u.commonRole))
		userID, role, err := account.Insert()
		if err != nil {
			if errors.Is(err, domain.ErrUsernameAlreadyTaken) || u.usernameConflict(err) {
				return domain.ErrGetAPICreateConflict
			}
			return domain.ErrGetAPICredentialUnavailable
		}
		if userID <= 0 {
			return domain.ErrGetAPICredentialUnavailable
		}
		if role >= principalRole {
			return domain.ErrGetAPICapabilityDenied
		}
		if request.InitialQuota != nil {
			if err := account.SetInitialQuota(*request.InitialQuota); err != nil {
				return domain.ErrGetAPICredentialUnavailable
			}
		}
		credential = &GetAPICreateCredential{UserID: userID, AccessToken: token, InitialQuota: account.Quota()}
		return nil
	})
	return credential, err
}
