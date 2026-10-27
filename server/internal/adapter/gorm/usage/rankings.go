package usage

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func (Store) GetRankingQuotaTotals(startTime int64, endTime int64) ([]domain.RankingQuotaTotal, error) {
	var rows []domain.RankingQuotaTotal
	query := model.DB.Table("quota_data").
		Select("model_name, sum(token_used) as total_tokens").
		Where("model_name <> ''").
		Group("model_name").
		Having("sum(token_used) > 0").
		Order("total_tokens DESC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func (Store) GetRankingQuotaBuckets(startTime int64, endTime int64, bucketSize int64) ([]domain.RankingQuotaBucket, error) {
	bucketExpr := rankingBucketExpr(bucketSize)
	var rows []domain.RankingQuotaBucket
	query := model.DB.Table("quota_data").
		Select(fmt.Sprintf("model_name, %s as bucket, sum(token_used) as tokens", bucketExpr)).
		Where("model_name <> ''").
		Group(fmt.Sprintf("model_name, %s", bucketExpr)).
		Having("sum(token_used) > 0").
		Order("bucket ASC")
	query = applyRankingQuotaTimeRange(query, startTime, endTime)
	err := query.Find(&rows).Error
	return rows, err
}

func rankingBucketExpr(bucketSize int64) string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return fmt.Sprintf("FLOOR(created_at / %d) * %d", bucketSize, bucketSize)
	}
	return fmt.Sprintf("(created_at / %d) * %d", bucketSize, bucketSize)
}

func applyRankingQuotaTimeRange(query *gorm.DB, startTime int64, endTime int64) *gorm.DB {
	if startTime > 0 {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("created_at <= ?", endTime)
	}
	return query
}

func (store Store) RankingMetadata() ([]domain.RankingVendor, []domain.RankingPrice) {
	vendors, prices := store.RankingCatalog()
	outVendors := make([]domain.RankingVendor, len(vendors))
	for i, item := range vendors {
		outVendors[i] = domain.RankingVendor{ID: item.ID, Name: item.Name, Icon: item.Icon}
	}
	outPrices := make([]domain.RankingPrice, len(prices))
	for i, item := range prices {
		outPrices[i] = domain.RankingPrice{ModelName: item.ModelName, VendorID: item.VendorID, OwnerBy: item.OwnerBy}
	}
	return outVendors, outPrices
}
