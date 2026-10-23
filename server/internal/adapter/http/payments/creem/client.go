package creem

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting"
)

type Client struct{}

func (Client) Verify(payload []byte, signature string) bool {
	if setting.CreemWebhookSecret == "" {
		logger.LogWarn(context.Background(), fmt.Sprintf("Creem webhook secret 未配置 test_mode=%t signature=%q body=%q", setting.CreemTestMode, signature, string(payload)))
		if setting.CreemTestMode {
			logger.LogInfo(context.Background(), fmt.Sprintf("Creem webhook 验签已跳过 reason=test_mode signature=%q body=%q", signature, string(payload)))
		}
		return setting.CreemTestMode
	}
	h := hmac.New(sha256.New, []byte(setting.CreemWebhookSecret))
	_, _ = h.Write(payload)
	return hmac.Equal([]byte(signature), []byte(hex.EncodeToString(h.Sum(nil))))
}

func (Client) Parse(payload []byte) (domain.CreemEvent, error) {
	var event CreemWebhookEvent
	if err := common.Unmarshal(payload, &event); err != nil {
		return domain.CreemEvent{}, err
	}
	return domain.CreemEvent{
		Type:         event.EventType,
		Reference:    event.Object.RequestId,
		Status:       event.Object.Order.Status,
		OrderType:    event.Object.Order.Type,
		Email:        event.Object.Customer.Email,
		CustomerName: event.Object.Customer.Name,
		ID:           event.Id, OrderID: event.Object.Order.Id, ProductName: event.Object.Product.Name, Currency: event.Object.Order.Currency, AmountPaid: event.Object.Order.AmountPaid,
		SubscriptionPayload: common.GetJsonString(&event),
	}, nil
}

type CreemWebhookEvent struct {
	Id        string `json:"id"`
	EventType string `json:"eventType"`
	CreatedAt int64  `json:"created_at"`
	Object    struct {
		Id        string `json:"id"`
		Object    string `json:"object"`
		RequestId string `json:"request_id"`
		Order     struct {
			Object      string `json:"object"`
			Id          string `json:"id"`
			Customer    string `json:"customer"`
			Product     string `json:"product"`
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			SubTotal    int    `json:"sub_total"`
			TaxAmount   int    `json:"tax_amount"`
			AmountDue   int    `json:"amount_due"`
			AmountPaid  int    `json:"amount_paid"`
			Status      string `json:"status"`
			Type        string `json:"type"`
			Transaction string `json:"transaction"`
			CreatedAt   string `json:"created_at"`
			UpdatedAt   string `json:"updated_at"`
			Mode        string `json:"mode"`
		} `json:"order"`
		Product struct {
			Id                string  `json:"id"`
			Object            string  `json:"object"`
			Name              string  `json:"name"`
			Description       string  `json:"description"`
			Price             int     `json:"price"`
			Currency          string  `json:"currency"`
			BillingType       string  `json:"billing_type"`
			BillingPeriod     string  `json:"billing_period"`
			Status            string  `json:"status"`
			TaxMode           string  `json:"tax_mode"`
			TaxCategory       string  `json:"tax_category"`
			DefaultSuccessUrl *string `json:"default_success_url"`
			CreatedAt         string  `json:"created_at"`
			UpdatedAt         string  `json:"updated_at"`
			Mode              string  `json:"mode"`
		} `json:"product"`
		Units    int `json:"units"`
		Customer struct {
			Id        string `json:"id"`
			Object    string `json:"object"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			Country   string `json:"country"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			Mode      string `json:"mode"`
		} `json:"customer"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Mode     string            `json:"mode"`
	} `json:"object"`
}

type CreemCheckoutRequest struct {
	ProductId string `json:"product_id"`
	RequestId string `json:"request_id"`
	Customer  struct {
		Email string `json:"email"`
	} `json:"customer"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CreemCheckoutResponse struct {
	CheckoutUrl string `json:"checkout_url"`
	Id          string `json:"id"`
}

func (Client) Checkout(ctx context.Context, referenceId string, product domain.CreemProduct, email string, username string) (string, error) {
	if setting.CreemApiKey == "" {
		return "", fmt.Errorf("Creem API key is not configured")
	}

	apiUrl := "https://api.creem.io/v1/checkouts"
	if setting.CreemTestMode {
		apiUrl = "https://test-api.creem.io/v1/checkouts"
		logger.LogInfo(ctx, fmt.Sprintf("Creem 使用测试环境 api_url=%s", apiUrl))
	}

	requestData := CreemCheckoutRequest{
		ProductId: product.ProductId,
		RequestId: referenceId,
		Customer: struct {
			Email string `json:"email"`
		}{
			Email: email,
		},
		Metadata: map[string]string{
			"username":     username,
			"reference_id": referenceId,
			"product_name": product.Name,
			"quota":        fmt.Sprintf("%d", product.Quota),
		},
	}

	jsonData, err := common.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("failed to serialize request: %v", err)
	}

	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", setting.CreemApiKey)

	logger.LogInfo(
		ctx,
		fmt.Sprintf("Creem 支付请求已发送 api_url=%s product_id=%s email=%q trade_no=%s", apiUrl, product.ProductId, email, referenceId),
	)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	logger.LogInfo(ctx, fmt.Sprintf("Creem API 响应已收到 trade_no=%s status_code=%d body=%q", referenceId, resp.StatusCode, string(body)))

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("Creem API http status %d ", resp.StatusCode)
	}

	var checkoutResp CreemCheckoutResponse
	err = common.Unmarshal(body, &checkoutResp)
	if err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	if checkoutResp.CheckoutUrl == "" {
		return "", fmt.Errorf("Creem API resp no checkout url ")
	}

	logger.LogInfo(
		ctx,
		fmt.Sprintf("Creem 支付链接创建成功 trade_no=%s response_id=%s checkout_url=%q", referenceId, checkoutResp.Id, checkoutResp.CheckoutUrl),
	)
	return checkoutResp.CheckoutUrl, nil
}
