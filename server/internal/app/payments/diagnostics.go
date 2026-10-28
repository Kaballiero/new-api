package payments

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/internal/payments/create_epay_topup"
	"github.com/QuantumNous/new-api/internal/payments/dto"
	"github.com/QuantumNous/new-api/internal/payments/receive_waffo_webhook"
	"github.com/QuantumNous/new-api/internal/payments/transport"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

func observeWebhook(c *gin.Context, provider string, payload []byte, signature string) {
	path, ip := "", ""
	if c != nil && c.Request != nil {
		if c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		ip = c.ClientIP()
	}
	logger.LogInfo(
		transport.Context(c),
		fmt.Sprintf("%s webhook request received path=%q client_ip=%s body_bytes=%d", provider, path, ip, len(payload)),
	)
}

func observeEpay(c *gin.Context, params map[string]string) {
	path, ip, method := "", "", ""
	if c != nil && c.Request != nil {
		if c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		ip = c.ClientIP()
		method = c.Request.Method
	}
	logger.LogInfo(
		transport.Context(c),
		fmt.Sprintf("Epay webhook request received path=%q client_ip=%s method=%s param_count=%d", path, ip, method, len(params)),
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
			"Epay topup order created user_id=%d trade_no=%s payment_method=%s amount=%d money=%.2f param_count=%d",
			userID,
			result.TradeNo,
			req.PaymentMethod,
			req.Amount,
			result.Money, len(result.Params),
		),
	)
}

func observeCreemPay(c *gin.Context, payload []byte) {
	userID := 0
	if c != nil {
		userID = c.GetInt("id")
	}
	logger.LogInfo(transport.Context(c), fmt.Sprintf("Creem payment request received user_id=%d body_bytes=%d", userID, len(payload)))
}

// waffoObservation retains the original SDK-build-before-payload-log boundary.
type waffoObservation struct{}

func (waffoObservation) Received(ctx context.Context, in receive_waffo_webhook.Input) {
	if ctx == nil {
		ctx = context.Background()
	}
	path, _, _ := strings.Cut(in.Path, "?")
	logger.LogInfo(ctx, fmt.Sprintf("Waffo webhook request received path=%q client_ip=%s body_bytes=%d", path, in.CallerIP, len(in.Payload)))
}

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
