package poll_batches_test

import (
	"testing"

	runtime "github.com/QuantumNous/new-api/internal/app/batchruntime"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchtickfixture"
)

func TestBatchTickContract(t *testing.T) {
	poll := runtime.New().Poll
	fixture.Run(t, "poll", "../../../service/testdata/batch", fixture.Hooks{
		RecordResult: poll.RecordResult, SettleResults: poll.SettleResults, ReconcilePurchase: poll.ReconcilePurchase,
	})
}
