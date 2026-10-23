package admin_list_user_usage_data

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type quotaReader struct {
	query domain.QuotaQuery
}

func (r *quotaReader) LegacyQuota(q domain.QuotaQuery) ([]*dto.QuotaData, error) {
	r.query = q
	return nil, nil
}

func (r *quotaReader) BucketUsers(q domain.QuotaQuery) ([]*dto.BucketedUserQuotaData, error) {
	r.query = q
	return nil, nil
}

func TestQuotaScopeIgnoresUsernameParameter(t *testing.T) {
	for _, granularity := range []string{"", domain.GranularityHour} {
		t.Run(granularity, func(t *testing.T) {
			reader := &quotaReader{}
			_, err := NewUsecase(reader).Execute(domain.QuotaQuery{
				StartTime: 1000, EndTime: 2000, Granularity: granularity,
				Username: "other",
			})
			require.NoError(t, err)
			assert.Empty(t, reader.query.Username, "this endpoint never accepted a username filter")
		})
	}
}
