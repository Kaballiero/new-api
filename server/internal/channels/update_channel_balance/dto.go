package update_channel_balance

import "github.com/QuantumNous/new-api/internal/channels/domain"

type Input struct{ ID int }
type Output = domain.BalanceResult
