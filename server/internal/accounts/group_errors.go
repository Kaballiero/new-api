package accounts

import "errors"

const MaxBulkGroupBatch = 1000

var (
	ErrInvalidGroupChange   = errors.New("invalid bulk group change")
	ErrGroupBatchTooLarge   = errors.New("bulk group change exceeds batch limit")
	ErrGroupDoesNotExist    = errors.New("group does not exist")
	ErrGroupUsersNotFound   = errors.New("bulk group users not found")
	ErrGroupChangeForbidden = errors.New("bulk group change is forbidden")
)
