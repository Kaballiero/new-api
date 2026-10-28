package redemption

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/redemption/record"
	"github.com/QuantumNous/new-api/internal/redemption/domain"

	"gorm.io/gorm"
)

// Store implements scenario persistence using the supplied database and transaction callbacks.
type Store struct {
	// db selects the current main database before each persistence operation.
	db func() *gorm.DB
	// lock adds the dialect row lock to the active redemption transaction.
	lock func(tx *gorm.DB) *gorm.DB
	// credit applies the shared wallet rule in the same transaction as the code consumption.
	credit func(tx *gorm.DB, userID int, quota int, updates map[string]any) error
}

func NewStore(db func() *gorm.DB, lock func(tx *gorm.DB) *gorm.DB,
	credit func(tx *gorm.DB, userID int, quota int, updates map[string]any) error) Store {
	return Store{db: db, lock: lock, credit: credit}
}

// Read preserves Begin, Count, Find, Commit and the legacy rollback-only panic recovery.
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

type pageReader struct {
	db *gorm.DB
}

func (r pageReader) query(in domain.Query) *gorm.DB {
	query := r.db.Model(&record.Redemption{})
	if in.Prefix != "" {
		if in.ID != nil {
			query = query.Where("id = ? OR name LIKE ?", *in.ID, in.Prefix+"%")
		} else {
			query = query.Where("name LIKE ?", in.Prefix+"%")
		}
	}
	if in.Status != nil {
		switch in.Expiry {
		case domain.PastExpiry:
			query = query.Where("status = ? AND expired_time != 0 AND expired_time < ?", *in.Status, in.Now)
		case domain.ActiveExpiry:
			query = query.Where("status = ? AND (expired_time = 0 OR expired_time >= ?)", *in.Status, in.Now)
		default:
			query = query.Where("status = ?", *in.Status)
		}
	}
	return query
}

func (r pageReader) Count(in domain.Query, total *int64) error {
	return r.query(in).Count(total).Error
}

func (r pageReader) Find(in domain.Query, start, limit int, codes *[]domain.Code) error {
	var rows []*record.Redemption
	// Populate the caller destination even if a query callback panics after scanning.
	// The outer read transaction retains the legacy rollback-only recovery.
	defer func() {
		if rows == nil {
			return
		}
		*codes = make([]domain.Code, len(rows))
		for i := range rows {
			(*codes)[i] = toCode(*rows[i])
		}
	}()
	return r.query(in).Order("id desc").Limit(limit).Offset(start).Find(&rows).Error
}

func (s Store) Get(id int) (domain.Code, error) {
	row := record.Redemption{Id: id}
	err := s.db().First(&row, "id = ?", id).Error
	return toCode(row), err
}

func (s Store) GetForDelete(id int) (domain.Code, error) {
	row := record.Redemption{Id: id}
	err := s.db().Where(row).First(&row).Error
	return toCode(row), err
}

func (s Store) Insert(code domain.Code) error {
	row := fromCode(code)
	return s.db().Create(&row).Error
}

func (s Store) Update(code domain.Code) error {
	row := fromCode(code)
	return s.db().Model(&row).Select("name", "status", "quota", "redeemed_time", "expired_time").Updates(&row).Error
}

func (s Store) Delete(code domain.Code) error {
	row := fromCode(code)
	return s.db().Delete(&row).Error
}

func (s Store) DeleteIDs(ids []int) (int64, error) {
	result := s.db().Where("id IN ?", ids).Delete(&record.Redemption{})
	return result.RowsAffected, result.Error
}

func (s Store) DeleteInvalid(in domain.InvalidCodes) (int64, error) {
	result := s.db().Where("status IN ? OR (status = ? AND expired_time != 0 AND expired_time < ?)",
		in.Statuses, in.ExpiringStatus, in.Before).Delete(&record.Redemption{})
	return result.RowsAffected, result.Error
}
