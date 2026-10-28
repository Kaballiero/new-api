package recover_batch_identity_test

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	gateway "github.com/QuantumNous/new-api/internal/adapter/gateway/batch"
	core "github.com/QuantumNous/new-api/internal/app/batch"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/recover_batch_identity"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/batchtickfixture"
)

func TestBatchTickContract(t *testing.T) {
	fixture.Run(t, "recovery", "../../../service/testdata/batch", fixture.Hooks{
		Verify: func(ctx context.Context, job *domain.Job, upstreamID, runnerID string) error {
			kinds := domain.ChannelKinds{OpenAI: constant.ChannelTypeOpenAI, Anthropic: constant.ChannelTypeAnthropic, Google: constant.ChannelTypeGemini, OpenRouter: constant.ChannelTypeOpenRouter}
			return recover_batch_identity.VerifyForTest(ctx, core.NewStore(), gateway.Gateway{}, kinds, job, upstreamID, runnerID)
		},
	})
}
