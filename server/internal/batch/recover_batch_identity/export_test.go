package recover_batch_identity

import (
	"context"
	"github.com/QuantumNous/new-api/internal/batch/domain"
)

// VerifyForTest exposes the private verification step only in this test binary.
func VerifyForTest(ctx context.Context, s Store, g domain.Gateway, k domain.ChannelKinds, job *domain.Job, upstreamID, runnerID string) error {
	return newVerifier(s, g, k).Execute(ctx, verificationInput{Job: job, UpstreamID: upstreamID, RunnerID: runnerID})
}
