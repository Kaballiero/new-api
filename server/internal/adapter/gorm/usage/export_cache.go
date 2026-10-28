package usage

import (
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
)

var CacheQuotaData = make(map[string]*record.QuotaData)

var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(quotaData *record.QuotaData) {
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%d\x00%s\x00%d\x00%d\x00%s",
		quotaData.UserID,
		quotaData.Username,
		quotaData.ModelName,
		quotaData.CreatedAt,
		quotaData.UseGroup,
		quotaData.TokenID,
		quotaData.ChannelID,
		quotaData.NodeName,
	)
	count := quotaData.Count
	quota := quotaData.Quota
	tokenUsed := quotaData.TokenUsed
	cachedQuotaData, ok := CacheQuotaData[key]
	if ok {
		cachedQuotaData.Count += count
		cachedQuotaData.Quota += quota
		cachedQuotaData.TokenUsed += tokenUsed
		quotaData = cachedQuotaData
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(params record.QuotaDataLogParams) {
	// Truncate the timestamp to the hour.
	createdAt := params.CreatedAt - (params.CreatedAt % 3600)
	count := 1
	if params.Correction {
		count = 0
	}
	quotaData := &record.QuotaData{
		UserID:    params.UserID,
		Username:  params.Username,
		ModelName: params.ModelName,
		CreatedAt: createdAt,
		UseGroup:  params.UseGroup,
		TokenID:   params.TokenID,
		ChannelID: params.ChannelID,
		NodeName:  params.NodeName,
		Count:     count,
		Quota:     params.Quota,
		TokenUsed: params.TokenUsed,
	}

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(quotaData)
}

// FlushQuotaDataCache retains the existing lock-through-I/O lifecycle for all producers.
func FlushQuotaDataCache(write func(*record.QuotaData), done func(int)) {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	size := len(CacheQuotaData)
	for _, row := range CacheQuotaData {
		write(row)
	}
	CacheQuotaData = make(map[string]*record.QuotaData)
	done(size)
}
