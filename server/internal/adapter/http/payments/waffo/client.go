package waffo

import (
	"context"
	"fmt"
	"strconv"

	"github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/config"
	"github.com/waffo-com/waffo-go/core"
	"github.com/waffo-com/waffo-go/types/order"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting"
)

type Factory struct{}

type Client struct{ sdk *waffo.Waffo }

func (Factory) Client() (domain.WaffoClient, error) {
	sdk, err := buildSDK()
	if err != nil {
		return nil, err
	}
	return Client{sdk: sdk}, nil
}

func buildSDK() (*waffo.Waffo, error) {
	env := config.Sandbox
	apiKey := setting.WaffoSandboxApiKey
	privateKey := setting.WaffoSandboxPrivateKey
	publicKey := setting.WaffoSandboxPublicCert
	if !setting.WaffoSandbox {
		env = config.Production
		apiKey = setting.WaffoApiKey
		privateKey = setting.WaffoPrivateKey
		publicKey = setting.WaffoPublicCert
	}
	builder := config.NewConfigBuilder().
		APIKey(apiKey).
		PrivateKey(privateKey).
		WaffoPublicKey(publicKey).
		Environment(env)
	if setting.WaffoMerchantId != "" {
		builder = builder.MerchantID(setting.WaffoMerchantId)
	}
	cfg, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return waffo.New(cfg), nil
}

type webhookPayloadWithSubInfo struct {
	EventType string `json:"eventType"`
	Result    struct {
		core.PaymentNotificationResult
		SubscriptionInfo *webhookSubscriptionInfo `json:"subscriptionInfo,omitempty"`
	} `json:"result"`
}

type webhookSubscriptionInfo struct {
	Period              string `json:"period,omitempty"`
	MerchantRequest     string `json:"merchantRequest,omitempty"`
	SubscriptionID      string `json:"subscriptionId,omitempty"`
	SubscriptionRequest string `json:"subscriptionRequest,omitempty"`
}

func (c Client) Checkout(ctx context.Context, in domain.WaffoCheckout) (string, error) {
	resp, err := c.sdk.Order().Create(
		ctx,
		&order.CreateOrderParams{
			PaymentRequestID:   in.TradeNo,
			MerchantOrderID:    in.TradeNo,
			OrderAmount:        in.Money,
			OrderCurrency:      in.Currency,
			OrderDescription:   in.Description,
			OrderRequestedAt:   in.RequestedAt,
			NotifyURL:          in.NotifyURL,
			MerchantInfo:       &order.MerchantInfo{MerchantID: in.MerchantID},
			UserInfo:           &order.UserInfo{UserID: strconv.Itoa(in.UserID), UserEmail: fmt.Sprintf("%d@examples.com", in.UserID), UserTerminal: "WEB"},
			PaymentInfo:        &order.PaymentInfo{ProductName: "ONE_TIME_PAYMENT", PayMethodType: in.MethodType, PayMethodName: in.MethodName},
			GoodsInfo:          &order.GoodsInfo{GoodsName: in.Description, AppName: in.AppName},
			SuccessRedirectURL: in.ReturnURL,
			FailedRedirectURL:  in.ReturnURL,
		},
		nil,
	)
	if err != nil {
		return "", err
	}
	if !resp.IsSuccess() {
		return "", domain.WaffoRejection{Code: resp.Code, Message: resp.Message, Response: common.GetJsonString(resp)}
	}
	data := resp.GetData()
	link := data.FetchRedirectURL()
	if link == "" {
		link = data.OrderAction
	}
	return link, nil
}

func (c Client) Verify(payload []byte, signature string) bool {
	return c.sdk.Webhook().VerifySignature(string(payload), signature)
}

func (c Client) Parse(payload []byte) (domain.WaffoEvent, error) {
	var event core.WebhookEvent
	if err := common.Unmarshal(payload, &event); err != nil {
		return domain.WaffoEvent{}, err
	}
	out := domain.WaffoEvent{Type: event.EventType}
	if event.EventType == core.EventPayment {
		var payment webhookPayloadWithSubInfo
		if err := common.Unmarshal(payload, &payment); err != nil {
			return domain.WaffoEvent{}, domain.WaffoPayloadError{EventType: event.EventType, Cause: err}
		}
		out.TradeNo = payment.Result.MerchantOrderID
		out.OrderStatus = payment.Result.OrderStatus
	}
	return out, nil
}

func (c Client) Reply(success bool, message string) domain.WaffoReply {
	var body, signature string
	if success {
		body, signature = c.sdk.Webhook().BuildSuccessResponse()
	} else {
		body, signature = c.sdk.Webhook().BuildFailedResponse(message)
	}
	return domain.WaffoReply{Body: body, Signature: signature}
}
