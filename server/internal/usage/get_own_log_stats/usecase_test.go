package get_own_log_stats

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/usage/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type statsReader struct {
	fail  bool
	query domain.LogQuery
	since int64
}

func (r *statsReader) LogStats(q domain.LogQuery, since func() int64) (domain.Stat, error) {
	r.query = q
	if r.fail {
		return domain.Stat{}, errors.New("invalid pattern")
	}
	r.since = since()
	return domain.Stat{}, nil
}

func TestOwnStatsIdentityAndLateRateWindow(t *testing.T) {
	reader := &statsReader{fail: true}
	clockReads := 0
	u := NewUsecase(reader, func() time.Time { clockReads++; return time.Unix(200, 0) })
	input := Input{Username: "authenticated", Query: domain.LogQuery{Username: "requested", Kind: domain.LogTypeError}}
	_, err := u.Execute(input)
	require.Error(t, err)
	assert.Zero(t, clockReads, "invalid patterns must not advance the rate-window clock")
	reader.fail = false
	_, err = u.Execute(input)
	require.NoError(t, err)
	assert.Equal(t, "authenticated", reader.query.Username)
	assert.Equal(t, domain.LogTypeConsume, reader.query.Kind)
	assert.Equal(t, int64(140), reader.since)
	assert.Equal(t, 1, clockReads)
}
