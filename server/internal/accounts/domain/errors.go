package domain

import "errors"

// MaxBulkGroupBatch limits the number of accounts in one bulk group change.
const MaxBulkGroupBatch = 1000

// Business errors of account scenarios. The transport maps each of them to the
// status, code and localized message of the HTTP response.
var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrManageForbidden      = errors.New("account role is not below the actor role")
	ErrViewForbidden        = errors.New("account is not visible to the actor role")
	ErrCreateForbidden      = errors.New("account role is not below the actor role on create")
	ErrRoleChangeNotAllowed = errors.New("account role cannot be changed by update")
	ErrInvalidAction        = errors.New("unknown account management action")
	ErrCannotDisableRoot    = errors.New("root account cannot be disabled")
	ErrCannotDeleteRoot     = errors.New("root account cannot be deleted")
	ErrAdminCannotPromote   = errors.New("only root can promote accounts")
	ErrAlreadyAdmin         = errors.New("account is already an administrator")
	ErrCannotDemoteRoot     = errors.New("root account cannot be demoted")
	ErrAlreadyCommon        = errors.New("account is already a common user")
	ErrUsernameTaken        = errors.New("username already taken")
	ErrBindingForbidden     = errors.New("binding of this account role cannot be cleared")
	// ErrAccountIDRequired is returned by account reads for the zero id.
	ErrAccountIDRequired = errors.New("account id is empty")

	ErrInvalidGroupChange   = errors.New("invalid bulk group change")
	ErrGroupBatchTooLarge   = errors.New("bulk group change exceeds batch limit")
	ErrGroupDoesNotExist    = errors.New("group does not exist")
	ErrGroupUsersNotFound   = errors.New("bulk group users not found")
	ErrGroupChangeForbidden = errors.New("bulk group change is forbidden")
	// ErrGroupChangeFailed classifies a storage failure of a bulk group change.
	// Create it with GroupChangeFailed.
	ErrGroupChangeFailed = errors.New("bulk group change failed")

	ErrQuotaChangeZero        = errors.New("quota change must be positive")
	ErrQuotaAdjustmentInvalid = errors.New("invalid user quota adjustment")
	// The text is shared with the wallet limit error of top-ups.
	ErrQuotaLimitExceeded = errors.New("wallet quota limit exceeded")

	ErrCheckinDisabled    = errors.New("check-in is disabled")
	ErrAlreadyCheckedIn   = errors.New("already checked in today")
	ErrCheckinFailed      = errors.New("check-in failed")
	ErrCheckinQuotaFailed = errors.New("check-in quota credit failed")

	// ErrAffiliateTransferFailed classifies a rejected affiliate quota transfer.
	// Create it with AffiliateTransferFailed.
	ErrAffiliateTransferFailed = errors.New("affiliate quota transfer failed")
	ErrTransferBelowMinimum    = errors.New("affiliate transfer quota is below the minimum")
	ErrInviteQuotaInsufficient = errors.New("affiliate quota is insufficient")

	// ErrSettingsSaveFailed classifies a failed write of account settings.
	// Create it with SettingsSaveFailed.
	ErrSettingsSaveFailed = errors.New("account settings save failed")
	ErrProfileInvalid     = errors.New("profile request is invalid")
	ErrProfileInputFailed = errors.New("profile input failed validation")
	// ErrPasswordChangeFailed classifies a failed own password change; its
	// cause is written by the security operation error writer. Create it with
	// PasswordChangeFailed.
	ErrPasswordChangeFailed = errors.New("password change failed")

	ErrNotifyTypeInvalid         = errors.New("notification type is invalid")
	ErrQuotaThresholdNotPositive = errors.New("quota warning threshold must be positive")
	ErrWebhookURLEmpty           = errors.New("webhook url is empty")
	ErrWebhookURLInvalid         = errors.New("webhook url is invalid")
	ErrNotificationEmailInvalid  = errors.New("notification email is invalid")
	ErrBarkURLEmpty              = errors.New("bark url is empty")
	ErrBarkURLInvalid            = errors.New("bark url is invalid")
	ErrNotificationURLNotHTTP    = errors.New("notification url must use http or https")
	ErrGotifyURLEmpty            = errors.New("gotify url is empty")
	ErrGotifyTokenEmpty          = errors.New("gotify token is empty")
	ErrGotifyURLInvalid          = errors.New("gotify url is invalid")

	// ErrInternal classifies an unexpected failure (usually storage). Create it
	// with Internal so that the message stays the text of the cause.
	ErrInternal = errors.New("internal error")
)

// Internal marks cause as an unexpected failure: errors.Is(err, ErrInternal)
// holds, errors.Unwrap returns cause and the message is the text of cause.
func Internal(cause error) error { return kindError{kind: ErrInternal, cause: cause} }

// GroupChangeFailed marks cause as a storage failure of a bulk group change.
func GroupChangeFailed(cause error) error { return kindError{kind: ErrGroupChangeFailed, cause: cause} }

// AffiliateTransferFailed marks cause as the reason of a rejected transfer.
func AffiliateTransferFailed(cause error) error {
	return kindError{kind: ErrAffiliateTransferFailed, cause: cause}
}

// SettingsSaveFailed marks cause as a failed write of account settings.
func SettingsSaveFailed(cause error) error {
	return kindError{kind: ErrSettingsSaveFailed, cause: cause}
}

// PasswordChangeFailed marks cause as the failure of an own password change.
func PasswordChangeFailed(cause error) error {
	return kindError{kind: ErrPasswordChangeFailed, cause: cause}
}

// kindError classifies cause by kind while keeping the text of cause.
type kindError struct{ kind, cause error }

func (e kindError) Error() string        { return e.cause.Error() }
func (e kindError) Unwrap() error        { return e.cause }
func (e kindError) Is(target error) bool { return target == e.kind }

// GroupError reports a rejected group; errors.Is matches Err.
type GroupError struct {
	Err   error
	Group string
}

func (e *GroupError) Error() string { return e.Err.Error() }
func (e *GroupError) Unwrap() error { return e.Err }

// AmountError reports a violated quota bound; Amount is the display value of
// the bound. errors.Is matches Err.
type AmountError struct {
	Err    error
	Amount string
}

func (e *AmountError) Error() string { return e.Err.Error() }
func (e *AmountError) Unwrap() error { return e.Err }
