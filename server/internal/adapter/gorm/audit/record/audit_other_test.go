package record_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"
)

func TestAuditOtherDatabaseEncoding(t *testing.T) {
	const payload = `{"op":{"action":"channel.update","params":{"id":9007199254740993,"nested":{"count":0,"enabled":false}}},"admin_info":{"admin_id":1,"admin_username":"root","admin_role":100,"auth_method":"session"},"audit_info":{"method":"PUT","route":"/api/channel/","path":"/api/channel/","status":200,"success":false},"root_info":{"generation":18446744073709551615}}`
	for _, input := range []any{payload, []byte(payload)} {
		var other record.AuditOther
		require.NoError(t, other.Scan(input))
		require.NotNil(t, other.Op)
		assert.Equal(t, "channel.update", other.Op.Action)
		require.NotNil(t, other.AdminInfo)
		assert.Equal(t, 100, other.AdminInfo.AdminRole)
		require.NotNil(t, other.AuditInfo)
		assert.False(t, other.AuditInfo.Success)
		encoded, err := other.Value()
		require.NoError(t, err)
		text, ok := encoded.(string)
		require.True(t, ok, "PostgreSQL simple protocol requires a string parameter")
		assert.JSONEq(t, payload, text)
		assert.Contains(t, text, "9007199254740993")
		assert.Contains(t, text, "18446744073709551615")
		for _, empty := range []any{nil, "", []byte(`null`), "{}"} {
			require.NoError(t, other.Scan(input))
			require.NoError(t, other.Scan(empty))
			encoded, err = other.Value()
			require.NoError(t, err)
			assert.Equal(t, "{}", encoded, "empty input must clear fields from the previous row")
		}
	}
	var invalid record.AuditOther
	assert.Error(t, invalid.Scan(42))
	assert.Error(t, invalid.Scan([]byte(`{broken`)))
}
