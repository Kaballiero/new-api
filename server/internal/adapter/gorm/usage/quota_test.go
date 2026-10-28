package usage

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/usage/admin_get_usage_flow"
	"github.com/QuantumNous/new-api/internal/usage/admin_list_usage_data"
	"github.com/QuantumNous/new-api/internal/usage/admin_list_user_usage_data"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/internal/usage/get_own_usage_flow"
	"github.com/QuantumNous/new-api/model"

	"github.com/glebarez/sqlite"
	sqlmysql "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/clickhouse"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func seedFlowQuotaData(t *testing.T, quotaData model.QuotaData) {
	t.Helper()
	require.NoError(t, model.DB.Create(&quotaData).Error)
}

func seedFlowLookupData(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: 2, Name: "west"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Delete(&model.Token{Id: 11}).Error)
}

func TestGetFlowQuotaDataUsesQuotaDataRoleSpecificDimensions(t *testing.T) {
	truncateTables(t)
	seedFlowLookupData(t)

	seedFlowQuotaData(t, model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 1,
		CreatedAt: 1000,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	})
	seedFlowQuotaData(t, model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 1,
		CreatedAt: 1100,
		Count:     1,
		Quota:     50,
		TokenUsed: 20,
	})
	seedFlowQuotaData(t, model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		UseGroup:  "vip",
		ModelName: "gpt-a",
		ChannelID: 2,
		CreatedAt: 1200,
		Count:     1,
		Quota:     25,
		TokenUsed: 10,
	})
	seedFlowQuotaData(t, model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		UseGroup:  "default",
		ModelName: "gpt-b",
		ChannelID: 1,
		CreatedAt: 1300,
		Count:     3,
		Quota:     70,
		TokenUsed: 30,
	})
	seedFlowQuotaData(t, model.QuotaData{
		UserID:    1,
		Username:  "alice",
		ModelName: "legacy",
		CreatedAt: 1400,
		Count:     99,
		Quota:     999,
		TokenUsed: 999,
	})

	rootRows, err := readFlowQuota(900, 2000, "", 0, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, rootRows, 3)
	// model.Token 11 was soft-deleted, so its name is intentionally left empty for the
	// frontend to render a localized "deleted (id)" label instead.
	require.Equal(t, dto.FlowQuotaData{
		UserID:      1,
		Username:    "alice",
		NodeName:    "node-a",
		TokenID:     11,
		TokenName:   "",
		UseGroup:    "vip",
		ChannelID:   1,
		ChannelName: "east",
		ModelName:   "gpt-a",
		TokenUsed:   60,
		Count:       3,
		Quota:       150,
	}, *rootRows[0])
	// A token that still exists resolves to its current name.
	require.Equal(t, 22, rootRows[1].TokenID)
	require.Equal(t, "backup", rootRows[1].TokenName)

	adminRows, err := readFlowQuota(900, 2000, "alice", 0, common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, adminRows, 2)
	require.Equal(t, 0, adminRows[0].TokenID)
	require.Empty(t, adminRows[0].TokenName)
	require.Empty(t, adminRows[0].NodeName)
	require.Equal(t, "alice", adminRows[0].Username)
	require.Equal(t, "vip", adminRows[0].UseGroup)
	require.Equal(t, "east", adminRows[0].ChannelName)
	require.Equal(t, 150, adminRows[0].Quota)

	selfRows, err := readFlowQuota(900, 2000, "", 1, common.RoleCommonUser)
	require.NoError(t, err)
	require.Len(t, selfRows, 1)
	require.Empty(t, selfRows[0].Username)
	require.Equal(t, 0, selfRows[0].ChannelID)
	require.Empty(t, selfRows[0].ChannelName)
	require.Empty(t, selfRows[0].TokenName)
	require.Equal(t, "vip", selfRows[0].UseGroup)
	require.Equal(t, 175, selfRows[0].Quota)
}

func TestLogQuotaDataSplitsRowsByUseGroupTokenChannelAndNode(t *testing.T) {
	truncateTables(t)
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*model.QuotaData)
	CacheQuotaDataLock.Unlock()

	LogQuotaData(record.QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3661,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     100,
		TokenUsed: 40,
	})
	LogQuotaData(record.QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "vip",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     50,
		TokenUsed: 20,
	})
	LogQuotaData(record.QuotaDataLogParams{
		UserID:    1,
		Username:  "alice",
		ModelName: "gpt-a",
		CreatedAt: 3700,
		UseGroup:  "default",
		TokenID:   11,
		ChannelID: 1,
		NodeName:  "node-a",
		Quota:     25,
		TokenUsed: 10,
	})

	(Store{}).SaveQuotaDataCache()

	var rows []model.QuotaData
	require.NoError(t, model.DB.Order("quota DESC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, int64(3600), rows[0].CreatedAt)
	require.Equal(t, "vip", rows[0].UseGroup)
	require.Equal(t, 11, rows[0].TokenID)
	require.Equal(t, 1, rows[0].ChannelID)
	require.Equal(t, "node-a", rows[0].NodeName)
	require.Equal(t, 2, rows[0].Count)
	require.Equal(t, 150, rows[0].Quota)
	require.Equal(t, 60, rows[0].TokenUsed)
	require.Equal(t, "default", rows[1].UseGroup)
	require.Equal(t, 25, rows[1].Quota)
}

func TestLogQuotaDataRefundsAreNetCorrectionsWithoutRequests(t *testing.T) {
	consumedAt := unixUTC(2026, time.May, 6, 10, 15, 0)
	laterRefundAt := unixUTC(2026, time.May, 6, 13, 5, 0)
	runOnEveryQuotaDialect(t, func(t *testing.T) {
		if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			require.NoError(t, model.DB.Exec("CREATE TABLE users (id BIGINT PRIMARY KEY)").Error)
			t.Cleanup(func() { model.DB.Exec("DROP TABLE users") })
			require.NoError(t, model.DB.Table("users").Create(map[string]any{"id": 1}).Error)
		} else {
			require.NoError(t, model.DB.Create(&model.User{Id: 1, Username: "alice", AffCode: "refund-net"}).Error)
		}
		CacheQuotaDataLock.Lock()
		CacheQuotaData = make(map[string]*record.QuotaData)
		CacheQuotaDataLock.Unlock()
		base := record.QuotaDataLogParams{UserID: 1, Username: "alice", ModelName: "video-a", UseGroup: "default", TokenID: 11, ChannelID: 1, NodeName: "node-a"}
		consume, sameHourRefund, laterRefund := base, base, base
		consume.CreatedAt, consume.Quota, consume.TokenUsed = consumedAt, 100, 40
		sameHourRefund.CreatedAt, sameHourRefund.Quota, sameHourRefund.Correction = consumedAt+60, -30, true
		laterRefund.CreatedAt, laterRefund.Quota, laterRefund.Correction = laterRefundAt, -20, true
		for _, params := range []record.QuotaDataLogParams{consume, sameHourRefund, laterRefund} {
			LogQuotaData(params)
		}
		(Store{}).SaveQuotaDataCache()

		var rows []record.QuotaData
		require.NoError(t, model.DB.Order("created_at").Find(&rows).Error)
		require.Len(t, rows, 2)
		assert.Equal(t, [4]int64{unixUTC(2026, time.May, 6, 10, 0, 0), 1, 70, 40}, [4]int64{rows[0].CreatedAt, int64(rows[0].Count), int64(rows[0].Quota), int64(rows[0].TokenUsed)})
		assert.Equal(t, [4]int64{unixUTC(2026, time.May, 6, 13, 0, 0), 0, -20, 0}, [4]int64{rows[1].CreatedAt, int64(rows[1].Count), int64(rows[1].Quota), int64(rows[1].TokenUsed)})

		daily, err := readBucketQuota(domain.QuotaQuery{StartTime: consumedAt - 3600, EndTime: laterRefundAt, Granularity: domain.GranularityDay, Grouping: domain.QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{{ModelName: "video-a", CreatedAt: unixUTC(2026, time.May, 6, 0, 0, 0), Count: 1, Quota: 50, TokenUsed: 40}}, daily)
		hourly, err := readBucketQuota(domain.QuotaQuery{StartTime: consumedAt - 3600, EndTime: laterRefundAt, Granularity: domain.GranularityHour, Grouping: domain.QuotaBucketByModel})
		require.NoError(t, err)
		require.Len(t, hourly, 2)
		assert.EqualValues(t, -20, hourly[1].Quota)
		assert.Zero(t, hourly[1].Count)

		start, end := consumedAt-3600, laterRefundAt
		aggregate, err := readUsageAggregate(domain.UsageAggregateQuery{AggregateScope: domain.AggregateScope{UserIDs: []int{1}, StartTimestamp: &start, EndTimestamp: &end}, GroupBy: "user", Granularity: "day"})
		require.NoError(t, err)
		require.Len(t, aggregate, 1)
		assert.Equal(t, [3]int64{50, 1, 40}, [3]int64{aggregate[0].Quota, aggregate[0].Count, aggregate[0].TokenUsed})
	})
}

// GORM treats a field named CreatedAt as autoCreateTime and replaces a zero
// value with the current time, so the bucket boundary at the epoch has to be
// written back explicitly.
func seedBucketQuotaData(t *testing.T, row model.QuotaData) {
	t.Helper()
	createdAt := row.CreatedAt
	require.NoError(t, model.DB.Create(&row).Error)
	require.NoError(t, model.DB.Table("quota_data").Where("id = ?", row.Id).Update("created_at", createdAt).Error)
}

func unixUTC(year int, month time.Month, day int, hour int, minute int, second int) int64 {
	return time.Date(year, month, day, hour, minute, second, 0, time.UTC).Unix()
}

// Buckets are the SQL mirror of the consumer-side bucketTimestamp() reduction;
// every supported database has to produce the same boundaries, so each case runs
// against SQLite plus any configured MySQL/PostgreSQL instance.
func runOnEveryQuotaDialect(t *testing.T, run func(*testing.T)) {
	t.Helper()
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			dsn := ""
			if kind != "sqlite" {
				dsn = os.Getenv("TEST_" + strings.ToUpper(kind) + "_DSN")
				if dsn == "" {
					t.Skip("host-only: database DSN not configured")
				}
			}
			setupReadDatabase(t, kind, dsn)
			run(t)
		})
	}
}

func TestGetBucketedQuotaDatesMatchesConsumerBucketBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		createdAt   int64
		granularity string
		expected    int64
	}{
		{"hour floors to the hour", unixUTC(2026, time.May, 6, 12, 37, 45), domain.GranularityHour, unixUTC(2026, time.May, 6, 12, 0, 0)},
		{"hour keeps an exact hour", unixUTC(2026, time.May, 6, 12, 0, 0), domain.GranularityHour, unixUTC(2026, time.May, 6, 12, 0, 0)},
		{"hour before an exact hour stays in the previous bucket", unixUTC(2026, time.May, 6,
			12, 0, 0) - 1, domain.GranularityHour, unixUTC(2026, time.May, 6, 11, 0, 0)},
		{"day floors to UTC midnight", unixUTC(2026, time.May, 6, 23, 59, 59), domain.GranularityDay, unixUTC(2026, time.May, 6, 0, 0, 0)},
		{"day keeps UTC midnight", unixUTC(2026, time.May, 6, 0, 0, 0), domain.GranularityDay, unixUTC(2026, time.May, 6, 0, 0, 0)},
		{"day before UTC midnight stays in the previous bucket", unixUTC(2026, time.May, 6, 0,
			0, 0) - 1, domain.GranularityDay, unixUTC(2026, time.May, 5, 0, 0, 0)},
		{"week from a Wednesday returns Monday", unixUTC(2026, time.May, 6, 12, 0, 0), domain.GranularityWeek, unixUTC(2026, time.May, 4, 0, 0, 0)},
		{"week from a Monday returns the same Monday", unixUTC(2026, time.May, 4, 5, 30, 0), domain.GranularityWeek, unixUTC(2026, time.May, 4, 0, 0, 0)},
		{"week from a Sunday returns the preceding Monday", unixUTC(2026, time.May, 10, 22, 0,
			0), domain.GranularityWeek, unixUTC(2026, time.May, 4, 0, 0, 0)},
		{"week avoids overflow at the int64 timestamp boundary", 9223372036854775807, domain.GranularityWeek, 9223372036854201600},
		{"week at the next Monday starts a new bucket", unixUTC(2026, time.May, 11, 0, 0, 0), domain.GranularityWeek, unixUTC(2026, time.May, 11, 0, 0, 0)},
		{"week at the epoch Thursday returns the preceding Monday", 0, domain.GranularityWeek, -259200},
		{"week at the last second before the first Monday", 345599, domain.GranularityWeek, -259200},
		{"week at the first Monday after the epoch", 345600, domain.GranularityWeek, 345600},
		{"month in a leap February", unixUTC(2024, time.February, 29, 12, 0, 0), domain.GranularityMonth, unixUTC(2024, time.February, 1, 0, 0, 0)},
		{"month at the last second of January", unixUTC(2026, time.January, 31, 23, 59, 59),
			domain.GranularityMonth, unixUTC(2026, time.January, 1, 0, 0, 0)},
		{"month at the last hour of the year", unixUTC(2025, time.December, 31, 23, 0, 0), domain.GranularityMonth,
			unixUTC(2025, time.December, 1, 0, 0, 0)},
		{"day at the last hour of the year", unixUTC(2025, time.December, 31, 23, 0, 0), domain.GranularityDay, unixUTC(2025, time.December, 31, 0, 0, 0)},
		{"month on the last day of a non-leap February", unixUTC(2026, time.February, 28, 23,
			0, 0), domain.GranularityMonth, unixUTC(2026, time.February, 1, 0, 0, 0)},
		{"month on the first day of March", unixUTC(2026, time.March, 1, 0, 0, 0), domain.GranularityMonth, unixUTC(2026, time.March, 1, 0, 0, 0)},
		{"month on the first hour of a new year", unixUTC(2026, time.January, 1, 0, 0, 0), domain.GranularityMonth,
			unixUTC(2026, time.January, 1, 0, 0, 0)},
		{"week on the first hour of a new year", unixUTC(2026, time.January, 1, 0, 0, 0), domain.GranularityWeek,
			unixUTC(2025, time.December, 29, 0, 0, 0)},
		{"week on the last hour of the previous year", unixUTC(2025, time.December, 31, 23, 0,
			0), domain.GranularityWeek, unixUTC(2025, time.December, 29, 0, 0, 0)},
	}

	runOnEveryQuotaDialect(t, func(t *testing.T) {
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				require.NoError(t, model.DB.Exec("DELETE FROM quota_data").Error)
				seedBucketQuotaData(t, model.QuotaData{UserID: 1, Username: "alice", ModelName: "gpt-a",
					CreatedAt: testCase.createdAt, Count: 1, Quota: 10, TokenUsed: 5})

				rows, err := readBucketQuota(domain.QuotaQuery{
					StartTime:   testCase.createdAt,
					EndTime:     testCase.createdAt,
					Granularity: testCase.granularity,
					Grouping:    domain.QuotaBucketByModel,
				})
				require.NoError(t, err)
				require.Len(t, rows, 1)
				assert.Equal(t, testCase.expected, rows[0].CreatedAt)
				assert.Equal(t, "gpt-a", rows[0].ModelName)
				assert.EqualValues(t, 1, rows[0].Count)

				byUser, err := readBucketUsers(domain.QuotaQuery{
					StartTime:   testCase.createdAt,
					EndTime:     testCase.createdAt,
					Granularity: testCase.granularity,
					Grouping:    domain.QuotaBucketByUser,
				})
				require.NoError(t, err)
				assert.Equal(t, []*dto.BucketedUserQuotaData{
					{Username: "alice", CreatedAt: testCase.expected, Count: 1, Quota: 10, TokenUsed: 5},
				}, byUser)
			})
		}
	})
}

func TestGetBucketedQuotaDatesPreservesLegacyTotalsPerModel(t *testing.T) {
	start := unixUTC(2026, time.April, 27, 0, 0, 0)
	end := unixUTC(2026, time.May, 10, 23, 0, 0)
	seed := []model.QuotaData{
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 1, Quota: 10, TokenUsed: 100},
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 23, 0, 0), Count: 2, Quota: 20, TokenUsed: 200},
		{UserID: 1, Username: "alice", ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 27, 23, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
		{UserID: 2, Username: "bob", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 30, 12, 0, 0), Count: 8, Quota: 80, TokenUsed: 800},
		{UserID: 2, Username: "bob", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
		{UserID: 2, Username: "bob", ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 10, 23, 0, 0), Count: 32, Quota: 320, TokenUsed: 3200},
	}

	runOnEveryQuotaDialect(t, func(t *testing.T) {
		for _, row := range seed {
			seedBucketQuotaData(t, row)
		}

		legacy, err := readLegacyQuota(start, end, "")
		require.NoError(t, err)
		legacyTotals := map[string][3]int64{}
		for _, row := range legacy {
			totals := legacyTotals[row.ModelName]
			legacyTotals[row.ModelName] = [3]int64{totals[0] + int64(row.Count), totals[1] + int64(row.Quota), totals[2] + int64(row.TokenUsed)}
		}
		require.Equal(t, map[string][3]int64{"gpt-a": {27, 270, 2700}, "gpt-b": {36, 360, 3600}}, legacyTotals)

		for _, granularity := range []string{domain.GranularityHour, domain.GranularityDay, domain.GranularityWeek, domain.GranularityMonth} {
			t.Run(granularity, func(t *testing.T) {
				rows, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: granularity, Grouping: domain.QuotaBucketByModel})
				require.NoError(t, err)
				bucketedTotals := map[string][3]int64{}
				for _, row := range rows {
					totals := bucketedTotals[row.ModelName]
					bucketedTotals[row.ModelName] = [3]int64{totals[0] + int64(row.Count), totals[1] + int64(row.Quota), totals[2] + int64(row.TokenUsed)}
					assert.Zero(t, row.UserID)
					assert.Empty(t, row.Username)
				}
				assert.Equal(t, legacyTotals, bucketedTotals)
			})
		}

		daily, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityDay,
			Grouping: domain.QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 3, Quota: 30, TokenUsed: 300},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 30, 0, 0, 0), Count: 8, Quota: 80, TokenUsed: 800},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 10, 0, 0, 0), Count: 32, Quota: 320, TokenUsed: 3200},
		}, daily)

		weekly, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityWeek,
			Grouping: domain.QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 11, Quota: 110, TokenUsed: 1100},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 32, Quota: 320, TokenUsed: 3200},
		}, weekly)

		monthly, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityMonth,
			Grouping: domain.QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 1, 0, 0, 0), Count: 11, Quota: 110, TokenUsed: 1100},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 1, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 1, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 1, 0, 0, 0), Count: 32, Quota: 320, TokenUsed: 3200},
		}, monthly)
	})
}

func TestGetBucketedQuotaDatesScopesRowsToTheRequestedIdentity(t *testing.T) {
	start := unixUTC(2026, time.May, 4, 0, 0, 0)
	end := unixUTC(2026, time.May, 10, 23, 0, 0)

	runOnEveryQuotaDialect(t, func(t *testing.T) {
		seedBucketQuotaData(t, model.QuotaData{UserID: 1, Username: "alice", TokenID: 11, ModelName: "gpt-a",
			CreatedAt: start, Count: 1, Quota: 10, TokenUsed: 100})
		seedBucketQuotaData(t, model.QuotaData{UserID: 1, Username: "alice", TokenID: 12, ModelName: "gpt-a",
			CreatedAt: start, Count: 2, Quota: 20, TokenUsed: 200})
		seedBucketQuotaData(t, model.QuotaData{UserID: 2, Username: "bob", TokenID: 22, ModelName: "gpt-a",
			CreatedAt: start, Count: 4, Quota: 40, TokenUsed: 400})

		byUser, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end, UserID: 1,
			Granularity: domain.GranularityWeek, Grouping: domain.QuotaBucketByUserAndModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: start, Count: 3, Quota: 30, TokenUsed: 300},
		}, byUser)

		byToken, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end, UserID: 1,
			TokenID: 12, Granularity: domain.GranularityWeek, Grouping: domain.QuotaBucketByUserAndModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: start, Count: 2, Quota: 20, TokenUsed: 200},
		}, byToken)

		byUsername, err := readBucketQuota(domain.QuotaQuery{StartTime: start, EndTime: end,
			Username: "bob", Granularity: domain.GranularityWeek, Grouping: domain.QuotaBucketByUserAndModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{UserID: 2, Username: "bob", ModelName: "gpt-a", CreatedAt: start, Count: 4, Quota: 40, TokenUsed: 400},
		}, byUsername)
	})
}

// The /api/data/users projection groups by username alone: rows of different
// models collapse into one user row, and neither model_name nor user_id is
// reported — exactly the legacy readLegacyUsers dimension set with
// created_at replaced by the bucket start.
func TestGetBucketedQuotaDatesByUserPreservesLegacyUserTotals(t *testing.T) {
	start := unixUTC(2026, time.April, 27, 0, 0, 0)
	end := unixUTC(2026, time.May, 10, 23, 0, 0)
	seed := []model.QuotaData{
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 1, Quota: 10, TokenUsed: 100},
		{UserID: 1, Username: "alice", ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 2, Quota: 20, TokenUsed: 200},
		{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 30, 12, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
		{UserID: 2, Username: "bob", ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 8, Quota: 80, TokenUsed: 800},
		{UserID: 2, Username: "bob", ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 10, 23, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
	}

	runOnEveryQuotaDialect(t, func(t *testing.T) {
		for _, row := range seed {
			seedBucketQuotaData(t, row)
		}

		legacy, err := readLegacyUsers(start, end)
		require.NoError(t, err)
		legacyTotals := map[string][3]int64{}
		for _, row := range legacy {
			totals := legacyTotals[row.Username]
			legacyTotals[row.Username] = [3]int64{totals[0] + int64(row.Count), totals[1] + int64(row.Quota), totals[2] + int64(row.TokenUsed)}
		}
		require.Equal(t, map[string][3]int64{"alice": {7, 70, 700}, "bob": {24, 240, 2400}}, legacyTotals)

		for _, granularity := range []string{domain.GranularityHour, domain.GranularityDay, domain.GranularityWeek, domain.GranularityMonth} {
			t.Run(granularity, func(t *testing.T) {
				rows, err := readBucketUsers(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: granularity, Grouping: domain.QuotaBucketByUser})
				require.NoError(t, err)
				bucketedTotals := map[string][3]int64{}
				for _, row := range rows {
					totals := bucketedTotals[row.Username]
					bucketedTotals[row.Username] = [3]int64{totals[0] + int64(row.Count), totals[1] + int64(row.Quota), totals[2] + int64(row.TokenUsed)}
				}
				assert.Equal(t, legacyTotals, bucketedTotals)
			})
		}

		daily, err := readBucketUsers(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityDay,
			Grouping: domain.QuotaBucketByUser})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedUserQuotaData{
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 3, Quota: 30, TokenUsed: 300},
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 30, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 8, Quota: 80, TokenUsed: 800},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 10, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
		}, daily)

		weekly, err := readBucketUsers(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityWeek,
			Grouping: domain.QuotaBucketByUser})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedUserQuotaData{
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 7, Quota: 70, TokenUsed: 700},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 24, Quota: 240, TokenUsed: 2400},
		}, weekly)

		monthly, err := readBucketUsers(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityMonth,
			Grouping: domain.QuotaBucketByUser})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedUserQuotaData{
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 1, 0, 0, 0), Count: 7, Quota: 70, TokenUsed: 700},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 1, 0, 0, 0), Count: 24, Quota: 240, TokenUsed: 2400},
		}, monthly)
	})
}

func TestGetBucketedQuotaDatesRejectsUnsupportedInput(t *testing.T) {
	truncateTables(t)

	_, err := readBucketQuota(domain.QuotaQuery{StartTime: 0, EndTime: 3600, Granularity: "quarter", Grouping: domain.QuotaBucketByModel})
	require.ErrorIs(t, err, domain.ErrInvalidQuotaGranularity)

	_, err = readBucketUsers(domain.QuotaQuery{StartTime: 0, EndTime: 3600, Granularity: "quarter", Grouping: domain.QuotaBucketByUser})
	require.ErrorIs(t, err, domain.ErrInvalidQuotaGranularity)

	_, err = readBucketQuota(domain.QuotaQuery{StartTime: 0, EndTime: 3600, Granularity: domain.GranularityHour})
	require.ErrorIs(t, err, domain.ErrInvalidQuotaGrouping)

	_, err = readBucketUsers(domain.QuotaQuery{StartTime: 0, EndTime: 3600, Granularity: domain.GranularityHour, Grouping: "everything"})
	require.ErrorIs(t, err, domain.ErrInvalidQuotaGrouping)

	start := unixUTC(2020, time.January, 1, 0, 0, 0)
	_, err = readBucketQuota(domain.QuotaQuery{
		StartTime:   start,
		EndTime:     unixUTC(2030, time.February, 1, 0, 0, 0),
		Granularity: domain.GranularityMonth,
		Grouping:    domain.QuotaBucketByModel,
	})
	require.ErrorIs(t, err, domain.ErrQuotaMonthSpanExceeded)

	_, err = readBucketUsers(domain.QuotaQuery{
		StartTime:   start,
		EndTime:     unixUTC(2030, time.February, 1, 0, 0, 0),
		Granularity: domain.GranularityMonth,
		Grouping:    domain.QuotaBucketByUser,
	})
	require.ErrorIs(t, err, domain.ErrQuotaMonthSpanExceeded)

	rows, err := readBucketQuota(domain.QuotaQuery{
		StartTime:   start,
		EndTime:     unixUTC(2029, time.December, 31, 23, 0, 0),
		Granularity: domain.GranularityMonth,
		Grouping:    domain.QuotaBucketByModel,
	})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestQuotaMonthBoundaries(t *testing.T) {
	januaryFirst := func(year int) int64 { return unixUTC(year, time.January, 1, 0, 0, 0) }

	for _, testCase := range []struct {
		name      string
		startTime int64
		endTime   int64
		expected  []int64
	}{
		{
			name:      "a single month yields its own start and the next one",
			startTime: unixUTC(2026, time.May, 6, 12, 0, 0),
			endTime:   unixUTC(2026, time.May, 6, 13, 0, 0),
			expected:  []int64{unixUTC(2026, time.May, 1, 0, 0, 0), unixUTC(2026, time.June, 1, 0, 0, 0)},
		},
		{
			name:      "the range start snaps back to the first of its month",
			startTime: unixUTC(2026, time.January, 31, 23, 59, 59),
			endTime:   unixUTC(2026, time.March, 1, 0, 0, 0),
			expected: []int64{
				unixUTC(2026, time.January, 1, 0, 0, 0),
				unixUTC(2026, time.February, 1, 0, 0, 0),
				unixUTC(2026, time.March, 1, 0, 0, 0),
				unixUTC(2026, time.April, 1, 0, 0, 0),
			},
		},
		{
			name:      "a boundary exactly on endTime still appends the following month",
			startTime: unixUTC(2026, time.January, 15, 0, 0, 0),
			endTime:   unixUTC(2026, time.February, 1, 0, 0, 0),
			expected: []int64{
				unixUTC(2026, time.January, 1, 0, 0, 0),
				unixUTC(2026, time.February, 1, 0, 0, 0),
				unixUTC(2026, time.March, 1, 0, 0, 0),
			},
		},
		{
			name:      "a leap February is one month like any other",
			startTime: unixUTC(2024, time.February, 29, 12, 0, 0),
			endTime:   unixUTC(2024, time.February, 29, 13, 0, 0),
			expected:  []int64{unixUTC(2024, time.February, 1, 0, 0, 0), unixUTC(2024, time.March, 1, 0, 0, 0)},
		},
		{
			name:      "a year rollover crosses into the next year",
			startTime: unixUTC(2025, time.December, 31, 23, 0, 0),
			endTime:   unixUTC(2026, time.January, 1, 0, 0, 0),
			expected: []int64{
				unixUTC(2025, time.December, 1, 0, 0, 0),
				januaryFirst(2026),
				unixUTC(2026, time.February, 1, 0, 0, 0),
			},
		},
		{
			name:      "endTime before startTime stops after the first step",
			startTime: unixUTC(2026, time.May, 6, 12, 0, 0),
			endTime:   unixUTC(2026, time.January, 1, 0, 0, 0),
			expected:  []int64{unixUTC(2026, time.May, 1, 0, 0, 0), unixUTC(2026, time.June, 1, 0, 0, 0)},
		},
		{
			name:      "startTime zero starts at the epoch month",
			startTime: 0,
			endTime:   unixUTC(1970, time.February, 15, 0, 0, 0),
			expected: []int64{
				unixUTC(1970, time.January, 1, 0, 0, 0),
				unixUTC(1970, time.February, 1, 0, 0, 0),
				unixUTC(1970, time.March, 1, 0, 0, 0),
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, domain.QuotaMonthBoundaries(testCase.startTime, testCase.endTime))
		})
	}

	t.Run("exactly domain.MaxBucketedMonths months is accepted", func(t *testing.T) {
		start := januaryFirst(2020)
		end := time.Unix(start, 0).UTC().AddDate(0, domain.MaxBucketedMonths, 0).Unix() - 1

		boundaries := domain.QuotaMonthBoundaries(start, end)

		require.Len(t, boundaries, domain.MaxBucketedMonths+1)
		assert.Equal(t, start, boundaries[0])
		assert.Equal(t, januaryFirst(2030), boundaries[domain.MaxBucketedMonths])
		expression, _, err := quotaBucketExpression(domain.GranularityMonth, start, end)
		require.NoError(t, err)
		assert.Equal(t, domain.MaxBucketedMonths, strings.Count(expression, " WHEN "))
	})

	t.Run("one month past domain.MaxBucketedMonths is rejected", func(t *testing.T) {
		start := januaryFirst(2020)
		end := time.Unix(start, 0).UTC().AddDate(0, domain.MaxBucketedMonths, 0).Unix()

		boundaries := domain.QuotaMonthBoundaries(start, end)

		require.Len(t, boundaries, domain.MaxBucketedMonths+2)
		err := domain.ValidateQuotaBucket(domain.QuotaQuery{StartTime: start, EndTime: end, Granularity: domain.GranularityMonth,
			Grouping: domain.QuotaBucketByModel})
		require.ErrorIs(t, err, domain.ErrQuotaMonthSpanExceeded)
	})

	t.Run("the boundary slice never grows past the cap guard", func(t *testing.T) {
		boundaries := domain.QuotaMonthBoundaries(januaryFirst(2020), januaryFirst(2500))

		assert.Len(t, boundaries, domain.MaxBucketedMonths+2)
	})
}

func TestGetBucketedQuotaDatesKeepsLargeExactSums(t *testing.T) {
	runOnEveryQuotaDialect(t, func(t *testing.T) {
		for range 2 {
			seedBucketQuotaData(t, model.QuotaData{UserID: 1, Username: "alice", ModelName: "a",
				CreatedAt: 1709168400, Count: 2147483647, Quota: 2147483647, TokenUsed: 2147483647})
		}
		for _, granularity := range []string{domain.GranularityHour, domain.GranularityDay, domain.GranularityWeek, domain.GranularityMonth} {
			rows, err := readBucketQuota(domain.QuotaQuery{StartTime: 1709168400, EndTime: 1709168400,
				Granularity: granularity, Grouping: domain.QuotaBucketByModel})
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, int64(4294967294), rows[0].Count)
			assert.Equal(t, int64(4294967294), rows[0].Quota)
			assert.Equal(t, int64(4294967294), rows[0].TokenUsed)
		}
	})
}

func TestGetBucketedQuotaDatesOrdersMonthBucketsNumerically(t *testing.T) {
	runOnEveryQuotaDialect(t, func(t *testing.T) {
		seedBucketQuotaData(t, model.QuotaData{UserID: 1, Username: "alice", ModelName: "a", CreatedAt: 999302400, Count: 1, Quota: 10, TokenUsed: 100})
		seedBucketQuotaData(t, model.QuotaData{UserID: 1, Username: "alice", ModelName: "a", CreatedAt: 1001894400, Count: 2, Quota: 20, TokenUsed: 200})
		rows, err := readBucketQuota(domain.QuotaQuery{StartTime: 999302400, EndTime: 1001894400,
			Granularity: domain.GranularityMonth, Grouping: domain.QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*dto.BucketedQuotaData{
			{ModelName: "a", CreatedAt: 999302400, Count: 1, Quota: 10, TokenUsed: 100},
			{ModelName: "a", CreatedAt: 1001894400, Count: 2, Quota: 20, TokenUsed: 200},
		}, rows)
	})
}

func truncateTables(t *testing.T) {
	setupReadDatabase(t, "sqlite", "")
}

func setupReadDatabase(t *testing.T, kind, dsn string) {
	t.Helper()
	mainDB := readTestDatabase(t, kind, dsn)
	logDB := readTestDatabase(t, kind, dsn)
	for _, db := range []*gorm.DB{mainDB, logDB} {
		require.NoError(t, db.AutoMigrate(&model.QuotaData{}, &model.Log{}, &model.Channel{}, &model.Token{}))
	}
	if kind == "sqlite" {
		require.NoError(t, mainDB.AutoMigrate(&model.User{}))
	}
	previousMain, previousLog := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMemory := common.RedisEnabled, common.MemoryCacheEnabled
	kindType := common.DatabaseTypeSQLite
	if kind == "mysql" {
		kindType = common.DatabaseTypeMySQL
	}
	if kind == "postgres" {
		kindType = common.DatabaseTypePostgreSQL
	}
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(kindType, kindType)
	common.RedisEnabled, common.MemoryCacheEnabled = false, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousMain, previousLog
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled, common.MemoryCacheEnabled = previousRedis, previousMemory
	})
}

func readTestDatabase(t *testing.T, kind, dsn string) *gorm.DB {
	t.Helper()
	if kind == "clickhouse" {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		admin, err := gorm.Open(clickhouse.Open(dsn), &gorm.Config{})
		require.NoError(t, err)
		adminSQL, err := admin.DB()
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, adminSQL.Close()) })
		name := "wave3a_" + strings.ReplaceAll(common.GetUUID(), "-", "")
		require.NoError(t, admin.Exec("CREATE DATABASE "+name).Error)
		t.Cleanup(func() { assert.NoError(t, admin.Exec("DROP DATABASE "+name).Error) })
		parsed.Path = "/" + name
		db, err := gorm.Open(clickhouse.Open(parsed.String()), &gorm.Config{})
		require.NoError(t, err)
		connection, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, connection.Close()) })
		var version string
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
		t.Logf("ClickHouse %s isolated database %s", version, name)
		return db
	}

	if kind == "postgres" {
		return usagePostgres(t, dsn)
	}
	if kind == "mysql" {
		config, err := sqlmysql.ParseDSN(dsn)
		require.NoError(t, err)
		config.DBName = ""
		admin, err := sql.Open("mysql", config.FormatDSN())
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, admin.Close()) })
		name := "wave3a_" + strings.ReplaceAll(common.GetUUID(), "-", "")
		_, err = admin.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
		require.NoError(t, err)
		t.Cleanup(func() { _, err := admin.Exec("DROP DATABASE `" + name + "`"); assert.NoError(t, err) })
		config.DBName = name
		db, err := gorm.Open(mysql.Open(config.FormatDSN()), &gorm.Config{})
		require.NoError(t, err)
		connection, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, connection.Close()) })
		var version string
		require.NoError(t, db.Raw("SELECT VERSION()").Scan(&version).Error)
		t.Logf("MySQL %s isolated database %s", version, name)
		return db
	}
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usage.db")), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, connection.Close()) })
	var version string
	require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	t.Logf("SQLite %s", version)
	return db
}

func readBucketQuota(q domain.QuotaQuery) ([]*dto.BucketedQuotaData, error) {
	if err := domain.ValidateQuotaBucket(q); err != nil {
		return nil, err
	}
	return (Store{}).BucketQuota(q)
}

func readBucketUsers(q domain.QuotaQuery) ([]*dto.BucketedUserQuotaData, error) {
	if err := domain.ValidateQuotaBucket(q); err != nil {
		return nil, err
	}
	return (Store{}).BucketUsers(q)
}

func readLegacyQuota(start, end int64, username string) ([]*dto.QuotaData, error) {
	result, err := admin_list_usage_data.NewUsecase(Store{}).Execute(domain.QuotaQuery{StartTime: start, EndTime: end, Username: username})
	return result.Legacy, err
}

func readLegacyUsers(start, end int64) ([]*dto.QuotaData, error) {
	result, err := admin_list_user_usage_data.NewUsecase(Store{}).Execute(domain.QuotaQuery{StartTime: start, EndTime: end})
	return result.Legacy, err
}

func readFlowQuota(start, end int64, username string, userID, role int) ([]*dto.FlowQuotaData, error) {
	query := domain.FlowQuery{StartTime: start, EndTime: end, Username: username, UserID: userID}
	if role < common.RoleAdminUser {
		return get_own_usage_flow.NewUsecase(Store{}).Execute(get_own_usage_flow.Input{Query: query})
	}
	audience := domain.AudienceAdmin
	if role >= common.RoleRootUser {
		audience = domain.AudienceRoot
	}
	return admin_get_usage_flow.NewUsecase(Store{}).Execute(admin_get_usage_flow.Input{Query: query, Audience: audience})
}
