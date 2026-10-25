package prepare_batch

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"

	"github.com/QuantumNous/new-api/internal/batch/domain"
)

var customID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type Store interface {
	ByOwnerKey(int, string) (*domain.Job, error)
}
type Validation interface {
	Provider([]byte) ([]byte, error)
	Item([]byte, string, string) (int, error)
}
type Input struct {
	Request        domain.CreateRequest
	Envelope       []byte
	UserID         int
	IdempotencyKey string
}
type Usecase struct {
	store      Store
	validation Validation
	codec      domain.Codec
	random     func(int) string
}

func NewUsecase(store Store, validation Validation, codec domain.Codec, random func(int) string) Usecase {
	return Usecase{store, validation, codec, random}
}
func (u Usecase) Execute(ctx context.Context, in Input) (domain.PreparedRequest, error) {
	var out domain.PreparedRequest
	request := in.Request
	routing, err := u.validation.Provider(in.Envelope)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.InvalidProvider, Cause: err}
	}
	request.Provider = routing
	if request.Model == "" || len(request.Model) > 255 || len(request.Requests) == 0 || len(request.Requests) > domain.MaxBatchRequests {
		return out, &domain.RuleError{Kind: domain.InvalidEnvelope}
	}
	seen := make(map[string]bool, len(request.Requests))
	for _, item := range request.Requests {
		if !customID.MatchString(item.CustomID) || seen[item.CustomID] {
			return out, &domain.RuleError{Kind: domain.InvalidCustomId}
		}
		seen[item.CustomID] = true
		if _, err := u.validation.Item(item.Body, request.Model, request.Endpoint); err != nil {
			return out, &domain.RuleError{Kind: domain.InvalidItem, ItemID: item.CustomID, Cause: err}
		}
	}
	publicID := "batch_" + u.random(32)
	key := in.IdempotencyKey
	if len(key) > 256 {
		return out, &domain.RuleError{Kind: domain.InvalidIdempotencyKey}
	}
	if key == "" {
		key = publicID
	}
	keyHash := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	data, err := u.codec.Marshal(request)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.InvalidPayload, Cause: err}
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	existing, err := u.store.ByOwnerKey(in.UserID, keyHash)
	if err == nil {
		if existing.RequestHash != hash {
			return out, &domain.RuleError{Kind: domain.IdempotencyConflict}
		}
		out.Existing = existing
	} else if !errors.Is(err, domain.ErrNotFound) {
		return out, &domain.RuleError{Kind: domain.StorageLookupUnavailable}
	}
	out.Request, out.PublicID, out.KeyHash, out.RequestHash = request, publicID, keyHash, hash
	return out, nil
}
