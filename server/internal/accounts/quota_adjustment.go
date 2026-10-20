package accounts

// UserQuotaAdjustment — снимок после зафиксированной ручной корректировки квоты.
type UserQuotaAdjustment struct {
	UserID   int
	Username string
	Role     int
	Status   int
	Before   int
	After    int
}
