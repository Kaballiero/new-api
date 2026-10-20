package domain

// DeletionSession связывает удаление аккаунта с текущими версиями пользователя и сессии.
type DeletionSession struct {
	UserID          int
	SessionID       string
	UserAuthVersion int64
	SessionVersion  int64
}
