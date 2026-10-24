package epay

import (
	"net/url"

	"github.com/Calcium-Ion/go-epay/epay"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payments/domain"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type Factory struct{}

func newClient() *epay.Client {
	if operation_setting.PayAddress == "" || operation_setting.EpayId == "" || operation_setting.EpayKey == "" {
		return nil
	}
	client, err := epay.NewClient(&epay.Config{PartnerID: operation_setting.EpayId, Key: operation_setting.EpayKey}, operation_setting.PayAddress)
	if err != nil {
		return nil
	}
	return client
}

func (Factory) Client() domain.EpayClient {
	client := newClient()
	if client == nil {
		return nil
	}
	return Client{client: client}
}

type Client struct{ client *epay.Client }

func (c Client) Purchase(in domain.EpayPurchase) (string, map[string]string, error) {
	notify, _ := url.Parse(in.NotifyURL)
	back, _ := url.Parse(in.ReturnURL)
	return c.client.Purchase(
		&epay.PurchaseArgs{Type: in.Method, ServiceTradeNo: in.TradeNo, Name: in.Name, Money: in.Money, Device: epay.PC, NotifyUrl: notify, ReturnUrl: back},
	)
}

func (c Client) Verify(params map[string]string) (domain.EpayEvent, error) {
	info, err := c.client.Verify(params)
	if err != nil {
		return domain.EpayEvent{}, err
	}
	return domain.EpayEvent{Verified: info.VerifyStatus, TradeNo: info.ServiceTradeNo, Method: info.Type, Status: info.TradeStatus, Verification: common.GetJsonString(info)}, nil
}
