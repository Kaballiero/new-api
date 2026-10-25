package create_batch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	billing "github.com/QuantumNous/new-api/internal/billing/domain"
)

type Store interface {
	CreateBatchReservation(*domain.Job, []domain.Item) error
	ByOwnerKey(int, string) (*domain.Job, error)
}
type Validation interface {
	Item([]byte, string, string) (int, error)
}
type Codec interface {
	domain.Codec
	EncodeSnapshot(domain.BillingSnapshot) ([]byte, error)
}
type Prices interface {
	Expression(string) (string, bool)
	Validate(string) error
	Group(string, string) float64
}
type Engine interface {
	billing.ExpressionEvaluator
	Provider(billing.ProviderBasis, float64) (int, *billing.Clamp, error)
	Hash(string) string
}
type Input struct{ Prepared domain.PreparedRequest }
type Result struct {
	Job       *domain.Job
	Duplicate bool
}
type Usecase struct {
	store      Store
	gateway    domain.AdmissionGateway
	codec      Codec
	validation Validation
	engine     Engine
	prices     Prices
	fx         func() (billing.FXBasis, error)
	enabled    func() bool
	now        func() time.Time
	openRouter int
	quotaUnit  func() float64
	maxWallet  int64
}

func NewUsecase(s Store, g domain.AdmissionGateway, c Codec, v Validation, e Engine, p Prices,
	fx func() (billing.FXBasis, error), enabled func() bool, now func() time.Time, openRouter int,
	quotaUnit func() float64, maxWallet int64) Usecase {
	return Usecase{s, g, c, v, e, p, fx, enabled, now, openRouter, quotaUnit, maxWallet}
}
func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	var out Result
	request := in.Prepared.Request
	session, err := u.gateway.Open(ctx)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.ChannelUnavailable}
	}
	if err := session.ValidateHeaders(); err != nil {
		return out, &domain.RuleError{Kind: domain.InvalidChannelHeaders, Cause: err}
	}
	if !u.enabled() {
		return out, &domain.RuleError{Kind: domain.BatchWorkerUnavailable}
	}
	facts, err := session.Initialize(request)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.InvalidModelMapping, Cause: err}
	}
	snapshot, priceErr := u.capture(request.Model, facts)
	if facts.ChannelType == u.openRouter {
		provider, err := session.Provider(request)
		if err != nil {
			return out, &domain.RuleError{Kind: domain.InvalidChannelParameters, Cause: err}
		}
		request.Provider = provider
	}
	if priceErr != nil {
		return out, &domain.RuleError{Kind: domain.BatchPricingUnavailable, Cause: priceErr}
	}
	snapshot.Provider = request.Provider
	items := make([]domain.Item, len(request.Requests))
	var total int64
	var inputBytes int
	for i, item := range request.Requests {
		body, err := session.PrepareBody(item)
		if err != nil {
			return out, err
		}
		inputBytes += len(body)
		if inputBytes > domain.MaxBatchRequestBytes {
			return out, &domain.RuleError{Kind: domain.BatchTooLarge}
		}
		maxOutput, err := u.validation.Item(body, facts.UpstreamModel, request.Endpoint)
		if err != nil {
			return out, &domain.RuleError{Kind: domain.InvalidChannelParameters, Cause: err}
		}
		var quota int
		if facts.ChannelType == u.openRouter {
			estimate, estimateErr := session.Estimate(body, snapshot.Provider)
			err = estimateErr
			if err == nil {
				var clamp *billing.Clamp
				quota, clamp, err = u.engine.Provider(*snapshot.OpenRouterCost, estimate)
				if clamp != nil {
					err = clamp
				}
				snapshot.OpenRouterCost.EstimatedUSD += estimate
			}
		} else {
			quota, err = u.estimate(session, snapshot, body, maxOutput)
		}
		if err != nil {
			return out, &domain.RuleError{Kind: domain.InvalidBatchPrice, Cause: err}
		}
		if total > u.maxWallet-int64(quota) {
			return out, &domain.RuleError{Kind: domain.BatchQuotaOverflow}
		}
		total += int64(quota)
		items[i] = domain.Item{CustomID: item.CustomID, Ordinal: i, Body: string(item.Body),
			UpstreamBody: string(body), BillingStatus: "reserved", ReservedQuota: int64(quota)}
	}
	encoded, err := u.codec.EncodeSnapshot(snapshot)
	if err != nil {
		return out, &domain.RuleError{Kind: domain.PricingUnavailable}
	}
	configHash, err := session.ConfigHash()
	if err != nil {
		return out, &domain.RuleError{Kind: domain.ChannelSnapshotUnavailable}
	}
	keyHash := session.CredentialHash()
	now := u.now().Unix()
	p := in.Prepared
	job := domain.Job{PublicID: p.PublicID, UserID: facts.UserID, TokenID: facts.TokenID,
		IdempotencyKey: p.KeyHash, RequestHash: p.RequestHash, ChannelID: facts.ChannelID,
		ChannelType: facts.ChannelType, CredentialIndex: facts.CredentialIndex, CredentialHash: keyHash,
		ChannelConfigHash: configHash, Model: request.Model, UsingGroup: facts.UsingGroup,
		UpstreamModel: facts.UpstreamModel, Endpoint: request.Endpoint, Status: "validating",
		BillingStatus: "reserved", SubmitState: "queued", BillingSnapshot: string(encoded),
		RequestCount: len(items), ReservedQuota: total, CreatedAt: now, UpdatedAt: now, ExpiresAt: now + 30*24*60*60}
	if err := u.store.CreateBatchReservation(&job, items); err != nil {
		existing, readErr := u.store.ByOwnerKey(facts.UserID, p.KeyHash)
		if readErr == nil && existing.RequestHash == p.RequestHash {
			return Result{existing, true}, nil
		}
		if errors.Is(err, domain.ErrBatchInsufficientQuota) {
			return out, &domain.RuleError{Kind: domain.InsufficientQuota, Cause: err}
		}
		return out, &domain.RuleError{Kind: domain.StorageReservationUnavailable}
	}
	return Result{&job, false}, nil
}
func (u Usecase) capture(model string, facts domain.AdmissionFacts) (domain.BillingSnapshot, error) {
	var snapshot domain.BillingSnapshot
	var expression string
	if facts.ChannelType != u.openRouter {
		var ok bool
		expression, ok = u.prices.Expression(model)
		if !ok {
			return snapshot, errors.New("an explicit batch billing_expr is required for this model")
		}
		if u.engine.UsedVars(expression)["header"] {
			return snapshot, errors.New("Batch billing expressions using header() are not supported")
		}
		if err := u.prices.Validate(expression); err != nil {
			return snapshot, fmt.Errorf("invalid batch billing_expr: %w", err)
		}
	}
	fx, err := u.fx()
	if err != nil {
		return snapshot, err
	}
	group := u.prices.Group(facts.UserGroup, facts.UsingGroup)
	effective, err := billing.ApplyBillingFX(group, fx.Factor)
	if err != nil {
		return snapshot, err
	}
	snapshot = domain.BillingSnapshot{EvaluatedAt: u.now().UTC(), FX: fx, PureGroupRatio: group, Headers: facts.Headers}
	if facts.ChannelType == u.openRouter {
		snapshot.SettlementMode = domain.BatchSettlementOpenRouterJobCostV2
		snapshot.OpenRouterCost = &billing.ProviderBasis{Version: domain.BatchSettlementOpenRouterJobCostV2,
			EffectiveGroupRatio: effective, QuotaPerUnit: u.quotaUnit()}
		return snapshot, nil
	}
	snapshot.Client = billing.TieredSnapshot{BillingMode: "tiered_expr", ModelName: model,
		ExprString: expression, ExprHash: u.engine.Hash(expression), ExprVersion: 1, GroupRatio: effective,
		QuotaPerUnit: u.quotaUnit()}
	snapshot.PurchaseExpression = expression
	return snapshot, nil
}

func (u Usecase) estimate(session domain.Admission, snapshot domain.BillingSnapshot, body []byte, maxOutput int) (int, error) {
	inputTokens, err := session.Count(snapshot.Client.ModelName, body)
	if err != nil {
		return 0, err
	}
	computed, err := u.engine.Compute(&snapshot.Client, billing.TokenParams{P: float64(inputTokens),
		Len: float64(inputTokens), C: float64(maxOutput)}, billing.RequestInput{Body: body,
		Headers: snapshot.Headers, EvaluatedAt: snapshot.EvaluatedAt})
	if err != nil {
		return 0, err
	}
	if computed.Clamp != nil {
		return 0, computed.Clamp
	}
	return computed.ActualQuotaAfterGroup, nil
}
