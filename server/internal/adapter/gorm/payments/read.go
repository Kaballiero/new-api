package payments

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/payments/record"
	"github.com/QuantumNous/new-api/internal/payments/domain"

	"gorm.io/gorm"
)

// Read preserves Begin/Commit errors and rollback-only panic recovery.
func (s Store) Read(run func(reader domain.PageReader) error) (err error) {
	tx := s.db().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if recover() != nil {
			tx.Rollback()
		}
	}()
	if err = run(pageReader{db: tx}); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit().Error
}

type pageReader struct{ db *gorm.DB }

func (r pageReader) query(in domain.Query) *gorm.DB {
	q := r.db.Model(&record.TopUp{})
	if in.UserID != nil {
		q = q.Where("user_id = ? AND create_time >= ?", *in.UserID, in.Cutoff)
	}
	if in.Pattern != "" {
		q = q.Where("trade_no LIKE ? ESCAPE '!'", in.Pattern)
	}
	return q
}

func (r pageReader) Count(in domain.Query, total *int64) error {
	q := r.query(in)
	if in.Search {
		q = q.Limit(in.CountLimit)
	}
	if err := q.Count(total).Error; err != nil {
		if in.Search {
			common.SysError("failed to count search topups: " + err.Error())
			return domain.ErrSearch
		}
		return err
	}
	return nil
}

func (r pageReader) Find(in domain.Query, start, limit int, orders *[]domain.Order) error {
	var rows []*record.TopUp
	defer func() {
		if rows != nil {
			*orders = make([]domain.Order, len(rows))
			for i, row := range rows {
				(*orders)[i] = toOrder(*row)
			}
		}
	}()
	if err := r.query(in).Order("id desc").Limit(limit).Offset(start).Find(&rows).Error; err != nil {
		if in.Search {
			common.SysError("failed to search topups: " + err.Error())
			return domain.ErrSearch
		}
		return err
	}
	return nil
}
