package payments

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/create_epay_topup"
	"github.com/QuantumNous/new-api/internal/payments/dto"

	"github.com/QuantumNous/new-api/internal/payments/transport"
	"github.com/QuantumNous/new-api/logger"
)

func observeEpay(c *gin.Context, params map[string]string) {
	path, ip, method := "", "", ""
	if c != nil && c.Request != nil {
		path = c.Request.RequestURI
		ip = c.ClientIP()
		method = c.Request.Method
	}
	logger.LogInfo(
		transport.Context(c),
		fmt.Sprintf("易支付 webhook 收到请求 path=%q client_ip=%s method=%s params=%q", path, ip, method, common.GetJsonString(params)),
	)
}

func observeEpayCheckout(c *gin.Context, req dto.EpayRequest, result create_epay_topup.Result) {
	userID := 0
	if c != nil {
		userID = c.GetInt("id")
	}
	logger.LogInfo(
		transport.Context(c),
		fmt.Sprintf(
			"易支付 充值订单创建成功 user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f uri=%q params=%q",
			userID,
			result.TradeNo,
			req.PaymentMethod,
			req.Amount,
			result.Money,
			result.URL,
			common.GetJsonString(result.Params),
		),
	)
}

// waffoObservation retains the original SDK-build-before-payload-log boundary.

// paymentDiagnostics writes request-correlated events without retaining a Gin context.
type paymentDiagnostics struct{}

func (paymentDiagnostics) Info(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogInfo(ctx, fmt.Sprintf(format, paymentLogValues(values)...))
}

func (paymentDiagnostics) Warn(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogWarn(ctx, fmt.Sprintf(format, paymentLogValues(values)...))
}

func (paymentDiagnostics) Error(ctx context.Context, format string, values ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogError(ctx, fmt.Sprintf(format, paymentLogValues(values)...))
}

// paymentLogValues retains legacy domain error bytes in the structural move.
func paymentLogValues(values []any) []any {
	for i, value := range values {
		if err, ok := value.(error); ok {
			values[i] = paymentLogError(err)
		}
	}
	return values
}
