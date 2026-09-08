package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	sqlitedriver "github.com/glebarez/go-sqlite"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func isUsernameUniqueViolation(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1062 && strings.Contains(strings.ToLower(mysqlErr.Message), "username")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && (strings.Contains(strings.ToLower(pgErr.ConstraintName), "username") || strings.Contains(strings.ToLower(pgErr.Detail), "username"))
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == 2067 && strings.Contains(strings.ToLower(err.Error()), "username")
	}
	return false
}

var (
	ErrGetAPIInvalidRequest        = errors.New("GETAPI_INVALID_REQUEST")
	ErrGetAPICapabilityDenied      = errors.New("GETAPI_CAPABILITY_DENIED")
	ErrGetAPIAccountNotFound       = errors.New("GETAPI_ACCOUNT_NOT_FOUND")
	ErrGetAPICreateConflict        = errors.New("GETAPI_CREATE_CONFLICT")
	ErrGetAPIUsernameMismatch      = errors.New("GETAPI_USERNAME_MISMATCH")
	ErrGetAPITargetRoleDenied      = errors.New("GETAPI_TARGET_ROLE_DENIED")
	ErrGetAPICredentialUnavailable = errors.New("GETAPI_CREDENTIAL_UNAVAILABLE")
)

type GetAPICreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}
type GetAPICredential struct {
	UserID      int     `json:"user_id"`
	State       string  `json:"state"`
	AccessToken *string `json:"access_token"`
}

type GetAPICreateCredential struct {
	UserID      int    `json:"user_id"`
	AccessToken string `json:"access_token"`
}
type GetAPIInitializePATRequest struct {
	UserID           int    `json:"user_id"`
	ExpectedUsername string `json:"expected_username"`
	Apply            bool   `json:"apply"`
}
type GetAPIInitializePATResult struct {
	UserID      int     `json:"user_id"`
	State       string  `json:"state"`
	Outcome     string  `json:"outcome"`
	AccessToken *string `json:"access_token"`
}

func ProvisionGetAPIUser(principalRole int, request GetAPICreateUserRequest) (*GetAPICreateCredential, error) {
	var credential *GetAPICreateCredential
	err := DB.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
		user := User{Username: strings.TrimSpace(request.Username), Password: request.Password, DisplayName: request.DisplayName, Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
		if user.DisplayName == "" {
			user.DisplayName = user.Username
		}
		if err := EnsureUsernameAvailableWithTx(tx, user.Username, 0); err != nil {
			if errors.Is(err, ErrUsernameAlreadyTaken) {
				return ErrGetAPICreateConflict
			}
			return ErrGetAPICredentialUnavailable
		}
		token, err := common.GenerateRandomKey(32)
		if err != nil {
			return ErrGetAPICredentialUnavailable
		}
		now := common.GetTimestamp()
		user.AccessToken = &token
		user.AccessTokenCreatedAt = &now
		setting := user.GetSetting()
		setting.SidebarModules = generateDefaultSidebarConfigForRole(user.Role)
		user.SetSetting(setting)
		if err := user.InsertWithTx(tx, 0); err != nil {
			if errors.Is(err, ErrUsernameAlreadyTaken) || isUsernameUniqueViolation(err) {
				return ErrGetAPICreateConflict
			}
			return ErrGetAPICredentialUnavailable
		}
		if user.Id <= 0 {
			return ErrGetAPICredentialUnavailable
		}
		if user.Role >= principalRole {
			return ErrGetAPICapabilityDenied
		}
		credential = &GetAPICreateCredential{UserID: user.Id, AccessToken: token}
		return nil
	})
	return credential, err
}

func InitializeGetAPIPAT(request GetAPIInitializePATRequest) (*GetAPIInitializePATResult, error) {
	var result *GetAPIInitializePATResult
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, request.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGetAPIAccountNotFound
			}
			return ErrGetAPICredentialUnavailable
		}
		if user.Role != common.RoleCommonUser {
			return ErrGetAPITargetRoleDenied
		}
		if user.Username != request.ExpectedUsername {
			return ErrGetAPIUsernameMismatch
		}
		if user.Status != common.UserStatusEnabled && user.Status != common.UserStatusDisabled {
			return ErrGetAPICredentialUnavailable
		}
		state := "active"
		if user.Status == common.UserStatusDisabled {
			state = "blocked"
		}
		token := strings.TrimRight(user.GetAccessToken(), " ")
		if token != "" {
			outcome := "would_reuse"
			if request.Apply {
				outcome = "reused"
			}
			result = &GetAPIInitializePATResult{UserID: user.Id, State: state, Outcome: outcome}
			if request.Apply {
				result.AccessToken = &token
			}
			return nil
		}
		if state == "blocked" {
			result = &GetAPIInitializePATResult{UserID: user.Id, State: state, Outcome: "blocked_no_pat"}
			return nil
		}
		result = &GetAPIInitializePATResult{UserID: user.Id, State: state, Outcome: "would_issue"}
		if !request.Apply {
			return nil
		}
		newToken, err := common.GenerateRandomKey(32)
		if err != nil {
			return ErrGetAPICredentialUnavailable
		}
		now := common.GetTimestamp()
		if err := tx.Model(&user).Updates(map[string]interface{}{"access_token": newToken, "access_token_created_at": now}).Error; err != nil {
			return ErrGetAPICredentialUnavailable
		}
		result.Outcome = "issued"
		result.AccessToken = &newToken
		return nil
	})
	return result, err
}
