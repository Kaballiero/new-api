package notify_quota

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/billing/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type noticeFixture struct{ notices []domain.Notice }

func (f *noticeFixture) Deliver(n domain.Notice) error { f.notices = append(f.notices, n); return nil }

func TestQuotaNotificationLateReadAndSubscriptionThreshold(t *testing.T) {
	f := &noticeFixture{}
	var queued func()
	threshold := 50
	state := domain.NotificationState{UserQuota: 200}
	u := NewUsecase(f, func() int { return threshold }, func(job func()) { queued = job }, quiet)
	require.NoError(t, u.Execute(Input{State: func() domain.NotificationState { return state }}))
	assert.Nil(t, queued, "zero actual usage does not queue a notification")
	require.NoError(t, u.Execute(Input{State: func() domain.NotificationState { return state }, Delta: 20, PreConsumed: 100}))
	threshold = 100
	queued()
	require.Len(t, f.notices, 1)
	assert.Equal(t, 200, f.notices[0].Remaining)
	state.SubscriptionID, state.SubscriptionTotal, state.SubscriptionUsedAfter, state.SubscriptionPostDelta = 1, 1000, 900, 50
	require.NoError(t, u.Execute(Input{State: func() domain.NotificationState { return state }, Delta: 1, Subscription: true}))
	queued()
	require.Len(t, f.notices, 2)
	assert.Equal(t, 50, f.notices[1].Remaining)
}

func quiet(string) {}
