package notifications

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/QuantumNous/new-api/common"
)

// telegramGatewayTextLimit is the gateway's text limit in UTF-16 code units.
const telegramGatewayTextLimit = 12000

// TelegramGateway sends texts to the Telegram chat of the GetAPI Telegram
// gateway. An empty URL turns the delivery off.
type TelegramGateway struct {
	url      func() string
	apiToken func() string
	client   *http.Client
}

func NewTelegramGateway(url func() string, apiToken func() string) TelegramGateway {
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return TelegramGateway{url: url, apiToken: apiToken, client: client}
}

func (g TelegramGateway) Send(text string) error {
	gatewayURL := g.url()
	if gatewayURL == "" {
		return nil
	}

	codeUnits := 0
	for offset, character := range text {
		codeUnits += utf16.RuneLen(character)
		if codeUnits > telegramGatewayTextLimit {
			text = text[:offset]
			break
		}
	}

	payloadBytes, err := common.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("failed to marshal telegram gateway payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimSuffix(gatewayURL, "/")+"/send", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create telegram gateway request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.apiToken())
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telegram gateway request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram gateway request failed with status code: %d", resp.StatusCode)
	}
	return nil
}
