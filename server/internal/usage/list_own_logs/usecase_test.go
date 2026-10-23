package list_own_logs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type ownedLogsReader struct {
	ownerError error
	reads      int
	query      domain.LogQuery
	policy     domain.VisibilityPolicy
}

func (r *ownedLogsReader) TokenOwner(token, user int) error {
	return r.ownerError
}

func (r *ownedLogsReader) ListOwnLogs(q domain.LogQuery, p domain.VisibilityPolicy) ([]*dto.Log, int64, error) {
	r.reads++
	r.query, r.policy = q, p
	return []*dto.Log{{Id: 1}}, 1, nil
}

func TestOwnLogsRejectForeignTokenBeforeRead(t *testing.T) {
	reader := &ownedLogsReader{ownerError: errors.New("foreign token")}
	u := NewUsecase(reader)
	_, err := u.Execute(Input{Query: domain.LogQuery{UserID: 1, TokenID: 2, ModelName: "a%b"}})
	require.ErrorIs(t, err, reader.ownerError)
	assert.Zero(t, reader.reads)
	reader.ownerError = nil
	result, err := u.Execute(Input{Query: domain.LogQuery{UserID: 1, TokenID: 2, ModelName: "a%b"}})
	require.NoError(t, err)
	assert.Equal(t, int64(1), result.Total)
	assert.True(t, reader.query.LiteralModel)
	assert.Equal(t, domain.MaxLogSearchCount, reader.query.CountLimit)
	assert.Equal(t, domain.LogVisibility(domain.AudienceUser), reader.policy)
}
