package receive_epay_notify

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/internal/payments/domain"
)

type Input struct {
	Params         map[string]string
	CallerIP, Path string
}

type Result struct{ Event domain.EpayEvent }

// SignatureFailure retains verification stage/cause for the original response/log ordering.
type SignatureFailure struct{ Cause error }

func (e SignatureFailure) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return domain.ErrInvalidParameters.Error()
}

type Usecase struct {
	diagnostics domain.Diagnostics
	clients     domain.EpayFactory
	completion  domain.Completion
	admission   domain.Admission
}

func NewUsecase(clients domain.EpayFactory, completion domain.Completion, admission domain.Admission, diagnostics domain.Diagnostics) Usecase {
	return Usecase{diagnostics: diagnostics, clients: clients, completion: completion, admission: admission}
}

func (u Usecase) Execute(ctx context.Context, in Input) (Result, error) {
	if len(in.Params) == 0 {
		u.diagnostics.Warn(ctx, "易支付 webhook 参数为空 path=%q client_ip=%s", in.Path, in.CallerIP)
		return Result{}, domain.ErrInvalidParameters
	}
	client := u.clients.Client()
	if client == nil {
		u.diagnostics.Error(ctx, "易支付 client 未初始化 path=%q client_ip=%s", in.Path, in.CallerIP)
		return Result{}, domain.ErrEpayConfig
	}
	event, err := client.Verify(in.Params)
	if err != nil || !event.Verified {
		return Result{}, SignatureFailure{Cause: err}
	}
	result := Result{Event: event}
	u.diagnostics.Info(ctx, "易支付 webhook 验签成功 trade_no=%s callback_type=%s trade_status=%s client_ip=%s verify_info=%q", event.TradeNo, event.Method, event.Status, in.CallerIP, event.Verification)
	if event.Status != "TRADE_SUCCESS" {
		u.diagnostics.Info(ctx, "易支付 webhook 忽略事件 trade_no=%s callback_type=%s trade_status=%s client_ip=%s verify_info=%q", event.TradeNo, event.Method, event.Status, in.CallerIP, event.Verification)
		return result, nil
	}
	u.admission.Lock(event.TradeNo)
	defer u.admission.Unlock(event.TradeNo)
	alreadyDone, err := u.completion.Complete(domain.CompletionInput{TradeNo: event.TradeNo, Provider: domain.PaymentProviderEpay, Method: event.Method, CallerIP: in.CallerIP})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrNotFound):
			u.diagnostics.Warn(ctx, "易支付 回调订单不存在 trade_no=%s callback_type=%s client_ip=%s verify_info=%q", event.TradeNo, event.Method, in.CallerIP, event.Verification)
		case errors.Is(err, domain.ErrProviderMismatch):
			u.diagnostics.Warn(ctx, "易支付 订单支付网关不匹配 trade_no=%s callback_type=%s client_ip=%s", event.TradeNo, event.Method, in.CallerIP)
		case errors.Is(err, domain.ErrNotPending):
			u.diagnostics.Warn(ctx, "易支付 订单状态非法 trade_no=%s callback_type=%s client_ip=%s", event.TradeNo, event.Method, in.CallerIP)
		default:
			u.diagnostics.Error(ctx, "易支付 充值处理失败 trade_no=%s client_ip=%s error=%q", event.TradeNo, in.CallerIP, err)
		}
		return result, err
	}
	if alreadyDone {
		u.diagnostics.Info(ctx, "易支付 重复回调幂等忽略 trade_no=%s callback_type=%s client_ip=%s", event.TradeNo, event.Method, in.CallerIP)
	} else {
		u.diagnostics.Info(ctx, "易支付 充值成功 trade_no=%s callback_type=%s client_ip=%s", event.TradeNo, event.Method, in.CallerIP)
	}
	return result, nil
}
