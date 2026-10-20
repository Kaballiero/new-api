package accounts

type AdminFailureKind string

const (
	AdminLoadMissing         AdminFailureKind = "load_missing"
	AdminLoadFailed          AdminFailureKind = "load_failed"
	AdminInvalidRole         AdminFailureKind = "invalid_role"
	AdminForbidden           AdminFailureKind = "forbidden"
	AdminCreateForbidden     AdminFailureKind = "create_forbidden"
	AdminWriteFailed         AdminFailureKind = "write_failed"
	AdminOrdinaryWriteFailed AdminFailureKind = "ordinary_write_failed"
	AdminPostCommitFailed    AdminFailureKind = "postcommit_failed"
	AdminInvalidAction       AdminFailureKind = "invalid_action"
	AdminCannotDisableRoot   AdminFailureKind = "cannot_disable_root"
	AdminCannotDeleteRoot    AdminFailureKind = "cannot_delete_root"
	AdminCannotPromote       AdminFailureKind = "admin_cannot_promote"
	AdminAlreadyAdmin        AdminFailureKind = "user_already_admin"
	AdminCannotDemoteRoot    AdminFailureKind = "cannot_demote_root"
	AdminAlreadyCommon       AdminFailureKind = "user_already_common"
)

type AdminFailure struct {
	Kind  AdminFailureKind
	Cause error
}

func (f *AdminFailure) Error() string {
	if f.Cause != nil {
		return f.Cause.Error()
	}
	return string(f.Kind)
}
func (f *AdminFailure) Unwrap() error { return f.Cause }

// AdminLoadFailure сохраняет исходный тип ошибки загрузки.
func AdminLoadFailure(err error) *AdminFailure {
	if failure, ok := err.(*AdminFailure); ok {
		return failure
	}
	return &AdminFailure{Kind: AdminLoadFailed, Cause: err}
}
