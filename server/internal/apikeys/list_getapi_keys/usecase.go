package list_getapi_keys

import "github.com/QuantumNous/new-api/internal/apikeys/domain"

// maxItems bounds the page size and the number of requested ids.
const maxItems = 100

type Reader interface {
	ListAfter(userID, afterID, limit int) ([]*domain.Token, error)
	ListByIDs(ids []int) ([]*domain.Token, error)
	Count(userID int) (int64, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

// Execute lists keys that are not deleted, newest first, with masked values.
func (usecase Usecase) Execute(input Input) (Output, error) {
	if (input.UserID == nil) == (input.IDs == nil) {
		return Output{}, domain.ErrListQueryInvalid
	}
	if input.IDs != nil {
		if input.AfterID != nil || input.Limit != nil || len(input.IDs) == 0 || len(input.IDs) > maxItems {
			return Output{}, domain.ErrListQueryInvalid
		}
		seen := make(map[int]bool, len(input.IDs))
		for _, id := range input.IDs {
			if id <= 0 || seen[id] {
				return Output{}, domain.ErrListQueryInvalid
			}
			seen[id] = true
		}
		tokens, err := usecase.reader.ListByIDs(input.IDs)
		if err != nil {
			return Output{}, domain.Internal(err)
		}
		return Output{Items: maskedKeys(tokens)}, nil
	}
	userID, afterID, limit := *input.UserID, 0, maxItems
	if input.AfterID != nil {
		afterID = *input.AfterID
		if afterID <= 0 {
			return Output{}, domain.ErrListQueryInvalid
		}
	}
	if input.Limit != nil {
		limit = *input.Limit
	}
	if userID <= 0 || limit < 1 || limit > maxItems {
		return Output{}, domain.ErrListQueryInvalid
	}
	tokens, err := usecase.reader.ListAfter(userID, afterID, limit)
	if err != nil {
		return Output{}, domain.Internal(err)
	}
	total, err := usecase.reader.Count(userID)
	if err != nil {
		return Output{}, domain.Internal(err)
	}
	output := Output{Items: maskedKeys(tokens), Total: total}
	if len(tokens) == limit {
		next := tokens[len(tokens)-1].Id
		output.NextAfterID = &next
	}
	return output, nil
}

// maskedKeys projects tokens without their full values; the result is never nil.
func maskedKeys(tokens []*domain.Token) []Key {
	keys := make([]Key, 0, len(tokens))
	for _, token := range tokens {
		keys = append(keys, Key{
			ID:             token.Id,
			UserID:         token.UserId,
			Name:           token.Name,
			MaskedKey:      domain.MaskKey(token.Key),
			Status:         token.Status,
			CreatedTime:    token.CreatedTime,
			AccessedTime:   token.AccessedTime,
			ExpiredTime:    token.ExpiredTime,
			RemainQuota:    token.RemainQuota,
			UsedQuota:      token.UsedQuota,
			UnlimitedQuota: token.UnlimitedQuota,
		})
	}
	return keys
}
