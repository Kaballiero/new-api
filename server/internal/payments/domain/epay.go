package domain

type EpayPurchase struct{ TradeNo, Method, Name, Money, NotifyURL, ReturnURL string }

type EpayEvent struct {
	Verified                              bool
	TradeNo, Method, Status, Verification string
}

// EpayClient signs checkout forms and verifies notification parameters using the same configured client.
type EpayClient interface {
	Purchase(purchase EpayPurchase) (string, map[string]string, error)
	Verify(params map[string]string) (EpayEvent, error)
}

// EpayFactory builds a fresh configured client, or nil when credentials/address cannot be used.
type EpayFactory interface{ Client() EpayClient }
