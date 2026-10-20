package domain

// InitialAccount задаётся после проверки email и подготовки пароля,
// внутри транзакции создания аккаунта.
type InitialAccount struct {
	Quota         int
	AffiliateCode string
}
