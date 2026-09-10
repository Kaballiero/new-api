package controller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeepSeekBalanceUSD(t *testing.T) {
	response := DeepSeekUsageResponse{}
	response.BalanceInfos = []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	}{
		{Currency: "USD", TotalBalance: "99.83"},
	}

	balance, err := deepSeekBalanceUSD(response)

	require.NoError(t, err)
	require.Equal(t, 99.83, balance)
}

func TestDeepSeekBalanceUSDRejectsCNYOnlyResponse(t *testing.T) {
	response := DeepSeekUsageResponse{}
	response.BalanceInfos = []struct {
		Currency        string `json:"currency"`
		TotalBalance    string `json:"total_balance"`
		GrantedBalance  string `json:"granted_balance"`
		ToppedUpBalance string `json:"topped_up_balance"`
	}{
		{Currency: "CNY", TotalBalance: "700.00"},
	}

	_, err := deepSeekBalanceUSD(response)

	require.EqualError(t, err, "currency USD not found")
}
