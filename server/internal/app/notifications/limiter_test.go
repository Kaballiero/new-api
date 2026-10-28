package notifications

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func TestNotificationLimitSelectsLateBackend(t *testing.T) {
	beforeRedis, beforeClient, beforeLimit := common.RedisEnabled, common.RDB, constant.NotifyLimitCount
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB, constant.NotifyLimitCount = beforeRedis, beforeClient, beforeLimit
	})
	constant.NotifyLimitCount = 0
	common.RedisEnabled = false
	allowed, err := checkNotificationLimit(901234567, "fixture_backend")
	require.NoError(t, err)
	require.False(t, allowed)
	client := redis.NewClient(&redis.Options{MaxRetries: -1, Dialer: func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("fixture Redis backend")
	}})
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	common.RDB, common.RedisEnabled = client, true
	_, err = checkNotificationLimit(901234567, "fixture_backend")
	require.ErrorContains(t, err, "fixture Redis backend")
}
