package redemption

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/redemption/record"
	"github.com/QuantumNous/new-api/internal/redemption/domain"
)

func toCode(row record.Redemption) domain.Code {
	return domain.Code{ID: row.Id, IssuerID: row.UserId, Key: row.Key, Status: row.Status, Name: row.Name, Quota: row.Quota,
		CreatedTime: row.CreatedTime, RedeemedTime: row.RedeemedTime, UsedUserID: row.UsedUserId, ExpiredTime: row.ExpiredTime}
}

func fromCode(code domain.Code) record.Redemption {
	return record.Redemption{Id: code.ID, UserId: code.IssuerID, Key: code.Key, Status: code.Status, Name: code.Name, Quota: code.Quota,
		CreatedTime: code.CreatedTime, RedeemedTime: code.RedeemedTime, UsedUserId: code.UsedUserID, ExpiredTime: code.ExpiredTime}
}
