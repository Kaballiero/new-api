package model

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedFlowQuotaData(t *testing.T, quotaData QuotaData) {
	t.Helper()
	require.NoError(t, DB.Create(&quotaData).Error)
}

func seedFlowLookupData(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "west"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, DB.Create(&Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, DB.Delete(&Token{Id: 11}).Error)
}

func TestGetFlowQuotaDataUsesQuotaDataRoleSpecificDimensions(t *testing.T) {
	truncateTables(t)
	seedFlowLookupData(t)

	seedFlowQuotaData(t, QuotaData{
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
	seedFlowQuotaData(t, QuotaData{
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
	seedFlowQuotaData(t, QuotaData{
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
	seedFlowQuotaData(t, QuotaData{
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
	seedFlowQuotaData(t, QuotaData{
		UserID:    1,
		Username:  "alice",
		ModelName: "legacy",
		CreatedAt: 1400,
		Count:     99,
		Quota:     999,
		TokenUsed: 999,
	})

	rootRows, err := GetFlowQuotaData(900, 2000, "", 0, common.RoleRootUser)
	require.NoError(t, err)
	require.Len(t, rootRows, 3)
	// Token 11 was soft-deleted, so its name is intentionally left empty for the
	// frontend to render a localized "deleted (id)" label instead.
	require.Equal(t, FlowQuotaData{
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

	adminRows, err := GetFlowQuotaData(900, 2000, "alice", 0, common.RoleAdminUser)
	require.NoError(t, err)
	require.Len(t, adminRows, 2)
	require.Equal(t, 0, adminRows[0].TokenID)
	require.Empty(t, adminRows[0].TokenName)
	require.Empty(t, adminRows[0].NodeName)
	require.Equal(t, "alice", adminRows[0].Username)
	require.Equal(t, "vip", adminRows[0].UseGroup)
	require.Equal(t, "east", adminRows[0].ChannelName)
	require.Equal(t, 150, adminRows[0].Quota)

	selfRows, err := GetFlowQuotaData(900, 2000, "", 1, common.RoleCommonUser)
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
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	LogQuotaData(QuotaDataLogParams{
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
	LogQuotaData(QuotaDataLogParams{
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
	LogQuotaData(QuotaDataLogParams{
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

	SaveQuotaDataCache()

	var rows []QuotaData
	require.NoError(t, DB.Order("quota DESC").Find(&rows).Error)
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

// GORM treats a field named CreatedAt as autoCreateTime and replaces a zero
// value with the current time, so the bucket boundary at the epoch has to be
// written back explicitly.
func seedBucketQuotaData(t *testing.T, row QuotaData) {
	t.Helper()
	createdAt := row.CreatedAt
	require.NoError(t, DB.Create(&row).Error)
	require.NoError(t, DB.Table("quota_data").Where("id = ?", row.Id).Update("created_at", createdAt).Error)
}

func unixUTC(year int, month time.Month, day int, hour int, minute int, second int) int64 {
	return time.Date(year, month, day, hour, minute, second, 0, time.UTC).Unix()
}

// Buckets are the SQL mirror of the consumer-side bucketTimestamp() reduction;
// every supported database has to produce the same boundaries, so each case runs
// against SQLite plus any configured MySQL/PostgreSQL instance.
func runOnEveryQuotaDialect(t *testing.T, run func(t *testing.T)) {
	t.Helper()
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			if dialect == "sqlite" {
				truncateTables(t)
				run(t)
				return
			}
			env := "TEST_MYSQL_DSN"
			if dialect == "postgres" {
				env = "TEST_POSTGRES_DSN"
			}
			dsn := os.Getenv(env)
			if dsn == "" {
				t.Skip("test database DSN is not configured")
			}
			t.Setenv("QUOTA_BUCKET_TEST_DSN", dsn)
			db, _, err := chooseDB("QUOTA_BUCKET_TEST_DSN", false)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, db.Migrator().DropTable(&QuotaData{}))
			require.NoError(t, db.AutoMigrate(&QuotaData{}))
			previous := DB
			DB = db
			t.Cleanup(func() {
				DB = previous
				_ = db.Migrator().DropTable(&QuotaData{})
				_ = sqlDB.Close()
			})
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
		{"hour floors to the hour", unixUTC(2026, time.May, 6, 12, 37, 45), GranularityHour, unixUTC(2026, time.May, 6, 12, 0, 0)},
		{"hour keeps an exact hour", unixUTC(2026, time.May, 6, 12, 0, 0), GranularityHour, unixUTC(2026, time.May, 6, 12, 0, 0)},
		{"hour before an exact hour stays in the previous bucket", unixUTC(2026, time.May, 6, 12, 0, 0) - 1, GranularityHour, unixUTC(2026, time.May, 6, 11, 0, 0)},
		{"day floors to UTC midnight", unixUTC(2026, time.May, 6, 23, 59, 59), GranularityDay, unixUTC(2026, time.May, 6, 0, 0, 0)},
		{"day keeps UTC midnight", unixUTC(2026, time.May, 6, 0, 0, 0), GranularityDay, unixUTC(2026, time.May, 6, 0, 0, 0)},
		{"day before UTC midnight stays in the previous bucket", unixUTC(2026, time.May, 6, 0, 0, 0) - 1, GranularityDay, unixUTC(2026, time.May, 5, 0, 0, 0)},
		{"week from a Wednesday returns Monday", unixUTC(2026, time.May, 6, 12, 0, 0), GranularityWeek, unixUTC(2026, time.May, 4, 0, 0, 0)},
		{"week from a Monday returns the same Monday", unixUTC(2026, time.May, 4, 5, 30, 0), GranularityWeek, unixUTC(2026, time.May, 4, 0, 0, 0)},
		{"week from a Sunday returns the preceding Monday", unixUTC(2026, time.May, 10, 22, 0, 0), GranularityWeek, unixUTC(2026, time.May, 4, 0, 0, 0)},
		{"week at the epoch Thursday returns the preceding Monday", 0, GranularityWeek, -259200},
		{"week at the last second before the first Monday", 345599, GranularityWeek, -259200},
		{"week at the first Monday after the epoch", 345600, GranularityWeek, 345600},
		{"month in a leap February", unixUTC(2024, time.February, 29, 12, 0, 0), GranularityMonth, unixUTC(2024, time.February, 1, 0, 0, 0)},
		{"month at the last second of January", unixUTC(2026, time.January, 31, 23, 59, 59), GranularityMonth, unixUTC(2026, time.January, 1, 0, 0, 0)},
		{"month at the last hour of the year", unixUTC(2025, time.December, 31, 23, 0, 0), GranularityMonth, unixUTC(2025, time.December, 1, 0, 0, 0)},
		{"day at the last hour of the year", unixUTC(2025, time.December, 31, 23, 0, 0), GranularityDay, unixUTC(2025, time.December, 31, 0, 0, 0)},
		{"month on the last day of a non-leap February", unixUTC(2026, time.February, 28, 23, 0, 0), GranularityMonth, unixUTC(2026, time.February, 1, 0, 0, 0)},
		{"month on the first day of March", unixUTC(2026, time.March, 1, 0, 0, 0), GranularityMonth, unixUTC(2026, time.March, 1, 0, 0, 0)},
		{"month on the first hour of a new year", unixUTC(2026, time.January, 1, 0, 0, 0), GranularityMonth, unixUTC(2026, time.January, 1, 0, 0, 0)},
		{"week on the first hour of a new year", unixUTC(2026, time.January, 1, 0, 0, 0), GranularityWeek, unixUTC(2025, time.December, 29, 0, 0, 0)},
		{"week on the last hour of the previous year", unixUTC(2025, time.December, 31, 23, 0, 0), GranularityWeek, unixUTC(2025, time.December, 29, 0, 0, 0)},
	}

	runOnEveryQuotaDialect(t, func(t *testing.T) {
		for _, testCase := range cases {
			t.Run(testCase.name, func(t *testing.T) {
				require.NoError(t, DB.Exec("DELETE FROM quota_data").Error)
				seedBucketQuotaData(t, QuotaData{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: testCase.createdAt, Count: 1, Quota: 10, TokenUsed: 5})

				rows, err := GetBucketedQuotaDates(BucketedQuotaQuery{
					StartTime:   testCase.createdAt,
					EndTime:     testCase.createdAt,
					Granularity: testCase.granularity,
					Grouping:    QuotaBucketByModel,
				})
				require.NoError(t, err)
				require.Len(t, rows, 1)
				assert.Equal(t, testCase.expected, rows[0].CreatedAt)
				assert.Equal(t, "gpt-a", rows[0].ModelName)
				assert.Equal(t, 1, rows[0].Count)

				byUser, err := GetBucketedQuotaDatesByUser(BucketedQuotaQuery{
					StartTime:   testCase.createdAt,
					EndTime:     testCase.createdAt,
					Granularity: testCase.granularity,
					Grouping:    QuotaBucketByUser,
				})
				require.NoError(t, err)
				assert.Equal(t, []*BucketedUserQuotaData{
					{Username: "alice", CreatedAt: testCase.expected, Count: 1, Quota: 10, TokenUsed: 5},
				}, byUser)
			})
		}
	})
}

func TestGetBucketedQuotaDatesPreservesLegacyTotalsPerModel(t *testing.T) {
	start := unixUTC(2026, time.April, 27, 0, 0, 0)
	end := unixUTC(2026, time.May, 10, 23, 0, 0)
	seed := []QuotaData{
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

		legacy, err := GetAllQuotaDates(start, end, "")
		require.NoError(t, err)
		legacyTotals := map[string][3]int{}
		for _, row := range legacy {
			totals := legacyTotals[row.ModelName]
			legacyTotals[row.ModelName] = [3]int{totals[0] + row.Count, totals[1] + row.Quota, totals[2] + row.TokenUsed}
		}
		require.Equal(t, map[string][3]int{"gpt-a": {27, 270, 2700}, "gpt-b": {36, 360, 3600}}, legacyTotals)

		for _, granularity := range []string{GranularityHour, GranularityDay, GranularityWeek, GranularityMonth} {
			t.Run(granularity, func(t *testing.T) {
				rows, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: granularity, Grouping: QuotaBucketByModel})
				require.NoError(t, err)
				bucketedTotals := map[string][3]int{}
				for _, row := range rows {
					totals := bucketedTotals[row.ModelName]
					bucketedTotals[row.ModelName] = [3]int{totals[0] + row.Count, totals[1] + row.Quota, totals[2] + row.TokenUsed}
					assert.Zero(t, row.UserID)
					assert.Empty(t, row.Username)
				}
				assert.Equal(t, legacyTotals, bucketedTotals)
			})
		}

		daily, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: GranularityDay, Grouping: QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedQuotaData{
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 3, Quota: 30, TokenUsed: 300},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 30, 0, 0, 0), Count: 8, Quota: 80, TokenUsed: 800},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 10, 0, 0, 0), Count: 32, Quota: 320, TokenUsed: 3200},
		}, daily)

		weekly, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: GranularityWeek, Grouping: QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedQuotaData{
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 11, Quota: 110, TokenUsed: 1100},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{ModelName: "gpt-a", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
			{ModelName: "gpt-b", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 32, Quota: 320, TokenUsed: 3200},
		}, weekly)

		monthly, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: GranularityMonth, Grouping: QuotaBucketByModel})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedQuotaData{
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
		seedBucketQuotaData(t, QuotaData{UserID: 1, Username: "alice", TokenID: 11, ModelName: "gpt-a", CreatedAt: start, Count: 1, Quota: 10, TokenUsed: 100})
		seedBucketQuotaData(t, QuotaData{UserID: 1, Username: "alice", TokenID: 12, ModelName: "gpt-a", CreatedAt: start, Count: 2, Quota: 20, TokenUsed: 200})
		seedBucketQuotaData(t, QuotaData{UserID: 2, Username: "bob", TokenID: 22, ModelName: "gpt-a", CreatedAt: start, Count: 4, Quota: 40, TokenUsed: 400})

		byUser, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, UserID: 1, Granularity: GranularityWeek, Grouping: QuotaBucketByUserAndModel})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedQuotaData{
			{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: start, Count: 3, Quota: 30, TokenUsed: 300},
		}, byUser)

		byToken, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, UserID: 1, TokenID: 12, Granularity: GranularityWeek, Grouping: QuotaBucketByUserAndModel})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedQuotaData{
			{UserID: 1, Username: "alice", ModelName: "gpt-a", CreatedAt: start, Count: 2, Quota: 20, TokenUsed: 200},
		}, byToken)

		byUsername, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: start, EndTime: end, Username: "bob", Granularity: GranularityWeek, Grouping: QuotaBucketByUserAndModel})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedQuotaData{
			{UserID: 2, Username: "bob", ModelName: "gpt-a", CreatedAt: start, Count: 4, Quota: 40, TokenUsed: 400},
		}, byUsername)
	})
}

// The /api/data/users projection groups by username alone: rows of different
// models collapse into one user row, and neither model_name nor user_id is
// reported — exactly the legacy GetQuotaDataGroupByUser dimension set with
// created_at replaced by the bucket start.
func TestGetBucketedQuotaDatesByUserPreservesLegacyUserTotals(t *testing.T) {
	start := unixUTC(2026, time.April, 27, 0, 0, 0)
	end := unixUTC(2026, time.May, 10, 23, 0, 0)
	seed := []QuotaData{
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

		legacy, err := GetQuotaDataGroupByUser(start, end)
		require.NoError(t, err)
		legacyTotals := map[string][3]int{}
		for _, row := range legacy {
			totals := legacyTotals[row.Username]
			legacyTotals[row.Username] = [3]int{totals[0] + row.Count, totals[1] + row.Quota, totals[2] + row.TokenUsed}
		}
		require.Equal(t, map[string][3]int{"alice": {7, 70, 700}, "bob": {24, 240, 2400}}, legacyTotals)

		for _, granularity := range []string{GranularityHour, GranularityDay, GranularityWeek, GranularityMonth} {
			t.Run(granularity, func(t *testing.T) {
				rows, err := GetBucketedQuotaDatesByUser(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: granularity, Grouping: QuotaBucketByUser})
				require.NoError(t, err)
				bucketedTotals := map[string][3]int{}
				for _, row := range rows {
					totals := bucketedTotals[row.Username]
					bucketedTotals[row.Username] = [3]int{totals[0] + row.Count, totals[1] + row.Quota, totals[2] + row.TokenUsed}
				}
				assert.Equal(t, legacyTotals, bucketedTotals)
			})
		}

		daily, err := GetBucketedQuotaDatesByUser(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: GranularityDay, Grouping: QuotaBucketByUser})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedUserQuotaData{
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 3, Quota: 30, TokenUsed: 300},
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 30, 0, 0, 0), Count: 4, Quota: 40, TokenUsed: 400},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 8, Quota: 80, TokenUsed: 800},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 10, 0, 0, 0), Count: 16, Quota: 160, TokenUsed: 1600},
		}, daily)

		weekly, err := GetBucketedQuotaDatesByUser(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: GranularityWeek, Grouping: QuotaBucketByUser})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedUserQuotaData{
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 27, 0, 0, 0), Count: 7, Quota: 70, TokenUsed: 700},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 4, 0, 0, 0), Count: 24, Quota: 240, TokenUsed: 2400},
		}, weekly)

		monthly, err := GetBucketedQuotaDatesByUser(BucketedQuotaQuery{StartTime: start, EndTime: end, Granularity: GranularityMonth, Grouping: QuotaBucketByUser})
		require.NoError(t, err)
		assert.Equal(t, []*BucketedUserQuotaData{
			{Username: "alice", CreatedAt: unixUTC(2026, time.April, 1, 0, 0, 0), Count: 7, Quota: 70, TokenUsed: 700},
			{Username: "bob", CreatedAt: unixUTC(2026, time.May, 1, 0, 0, 0), Count: 24, Quota: 240, TokenUsed: 2400},
		}, monthly)
	})
}

func TestGetBucketedQuotaDatesRejectsUnsupportedInput(t *testing.T) {
	truncateTables(t)

	_, err := GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: 0, EndTime: 3600, Granularity: "quarter", Grouping: QuotaBucketByModel})
	require.ErrorIs(t, err, ErrInvalidQuotaGranularity)

	_, err = GetBucketedQuotaDatesByUser(BucketedQuotaQuery{StartTime: 0, EndTime: 3600, Granularity: "quarter", Grouping: QuotaBucketByUser})
	require.ErrorIs(t, err, ErrInvalidQuotaGranularity)

	_, err = GetBucketedQuotaDates(BucketedQuotaQuery{StartTime: 0, EndTime: 3600, Granularity: GranularityHour})
	require.ErrorIs(t, err, ErrInvalidQuotaGrouping)

	_, err = GetBucketedQuotaDatesByUser(BucketedQuotaQuery{StartTime: 0, EndTime: 3600, Granularity: GranularityHour, Grouping: "everything"})
	require.ErrorIs(t, err, ErrInvalidQuotaGrouping)

	start := unixUTC(2020, time.January, 1, 0, 0, 0)
	_, err = GetBucketedQuotaDates(BucketedQuotaQuery{
		StartTime:   start,
		EndTime:     unixUTC(2030, time.February, 1, 0, 0, 0),
		Granularity: GranularityMonth,
		Grouping:    QuotaBucketByModel,
	})
	require.ErrorIs(t, err, ErrQuotaMonthSpanExceeded)

	_, err = GetBucketedQuotaDatesByUser(BucketedQuotaQuery{
		StartTime:   start,
		EndTime:     unixUTC(2030, time.February, 1, 0, 0, 0),
		Granularity: GranularityMonth,
		Grouping:    QuotaBucketByUser,
	})
	require.ErrorIs(t, err, ErrQuotaMonthSpanExceeded)

	rows, err := GetBucketedQuotaDates(BucketedQuotaQuery{
		StartTime:   start,
		EndTime:     unixUTC(2029, time.December, 31, 23, 0, 0),
		Granularity: GranularityMonth,
		Grouping:    QuotaBucketByModel,
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
			assert.Equal(t, testCase.expected, quotaMonthBoundaries(testCase.startTime, testCase.endTime))
		})
	}

	t.Run("exactly MaxBucketedMonths months is accepted", func(t *testing.T) {
		start := januaryFirst(2020)
		end := time.Unix(start, 0).UTC().AddDate(0, MaxBucketedMonths, 0).Unix() - 1

		boundaries := quotaMonthBoundaries(start, end)

		require.Len(t, boundaries, MaxBucketedMonths+1)
		assert.Equal(t, start, boundaries[0])
		assert.Equal(t, januaryFirst(2030), boundaries[MaxBucketedMonths])
		expression, err := quotaBucketExpression(GranularityMonth, start, end)
		require.NoError(t, err)
		assert.Equal(t, MaxBucketedMonths, strings.Count(expression, " WHEN "))
	})

	t.Run("one month past MaxBucketedMonths is rejected", func(t *testing.T) {
		start := januaryFirst(2020)
		end := time.Unix(start, 0).UTC().AddDate(0, MaxBucketedMonths, 0).Unix()

		boundaries := quotaMonthBoundaries(start, end)

		require.Len(t, boundaries, MaxBucketedMonths+2)
		_, err := quotaBucketExpression(GranularityMonth, start, end)
		require.ErrorIs(t, err, ErrQuotaMonthSpanExceeded)
	})

	t.Run("the boundary slice never grows past the cap guard", func(t *testing.T) {
		boundaries := quotaMonthBoundaries(januaryFirst(2020), januaryFirst(2500))

		assert.Len(t, boundaries, MaxBucketedMonths+2)
	})
}
