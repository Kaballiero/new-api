package notifications

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/notifications/domain"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
)

type Delivery struct {
	WorkerEnabled func() bool
	WorkerKey     func() string
	Worker        func(*httpclient.WorkerRequest) (*http.Response, error)
	Validate      func(string) error
	Client        func() *http.Client
}

func (d Delivery) Email(userEmail string, data domain.Notice) error {
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, domain.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}
	return common.SendEmail(data.Title, userEmail, content)
}

func (d Delivery) Bark(barkURL string, data domain.Notice) error {
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, domain.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}

	finalURL := strings.ReplaceAll(barkURL, "{{title}}", url.QueryEscape(data.Title))
	finalURL = strings.ReplaceAll(finalURL, "{{content}}", url.QueryEscape(content))

	var req *http.Request
	var resp *http.Response
	var err error

	if d.WorkerEnabled() {
		workerReq := &httpclient.WorkerRequest{
			URL:    finalURL,
			Key:    d.WorkerKey(),
			Method: http.MethodGet,
			Headers: map[string]string{
				"User-Agent": "OneAPI-Bark-Notify/1.0",
			},
		}

		resp, err = d.Worker(workerReq)
		if err != nil {
			return fmt.Errorf("failed to send bark request through worker: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("bark request failed with status code: %d", resp.StatusCode)
		}
	} else {
		if err := d.Validate(finalURL); err != nil {
			return fmt.Errorf("request reject: %v", err)
		}

		req, err = http.NewRequest(http.MethodGet, finalURL, nil)
		if err != nil {
			return fmt.Errorf("failed to create bark request: %v", err)
		}

		req.Header.Set("User-Agent", "OneAPI-Bark-Notify/1.0")

		client := d.Client()
		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send bark request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("bark request failed with status code: %d", resp.StatusCode)
		}
	}

	return nil
}

func (d Delivery) Gotify(gotifyUrl string, gotifyToken string, priority int, data domain.Notice) error {
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, domain.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}

	finalURL := strings.TrimSuffix(gotifyUrl, "/") + "/message?token=" + url.QueryEscape(gotifyToken)

	if priority < 0 || priority > 10 {
		priority = 5
	}

	type GotifyMessage struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	}

	payload := GotifyMessage{
		Title:    data.Title,
		Message:  content,
		Priority: priority,
	}

	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal gotify payload: %v", err)
	}

	var req *http.Request
	var resp *http.Response

	if d.WorkerEnabled() {
		workerReq := &httpclient.WorkerRequest{
			URL:    finalURL,
			Key:    d.WorkerKey(),
			Method: http.MethodPost,
			Headers: map[string]string{
				"Content-Type": "application/json; charset=utf-8",
				"User-Agent":   "OneAPI-Gotify-Notify/1.0",
			},
			Body: payloadBytes,
		}

		resp, err = d.Worker(workerReq)
		if err != nil {
			return fmt.Errorf("failed to send gotify request through worker: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("gotify request failed with status code: %d", resp.StatusCode)
		}
	} else {
		if err := d.Validate(finalURL); err != nil {
			return fmt.Errorf("request reject: %v", err)
		}

		req, err = http.NewRequest(http.MethodPost, finalURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			return fmt.Errorf("failed to create gotify request: %v", err)
		}

		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("User-Agent", "NewAPI-Gotify-Notify/1.0")

		client := d.Client()
		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send gotify request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("gotify request failed with status code: %d", resp.StatusCode)
		}
	}

	return nil
}

type WebhookPayload struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Values    []any  `json:"values,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

func generateSignature(secret string, payload []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func (d Delivery) Webhook(webhookURL string, secret string, data domain.Notice) error {
	content := data.Content
	for _, value := range data.Values {
		content = fmt.Sprintf(content, value)
	}

	payload := WebhookPayload{
		Type:      data.Type,
		Title:     data.Title,
		Content:   content,
		Values:    data.Values,
		Timestamp: time.Now().Unix(),
	}

	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %v", err)
	}

	var req *http.Request
	var resp *http.Response

	if d.WorkerEnabled() {
		workerReq := &httpclient.WorkerRequest{
			URL:    webhookURL,
			Key:    d.WorkerKey(),
			Method: http.MethodPost,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
			Body: payloadBytes,
		}

		if secret != "" {
			signature := generateSignature(secret, payloadBytes)
			workerReq.Headers["X-Webhook-Signature"] = signature
			workerReq.Headers["Authorization"] = "Bearer " + secret
		}

		resp, err = d.Worker(workerReq)
		if err != nil {
			return fmt.Errorf("failed to send webhook request through worker: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("webhook request failed with status code: %d", resp.StatusCode)
		}
	} else {
		if err := d.Validate(webhookURL); err != nil {
			return fmt.Errorf("request reject: %v", err)
		}

		req, err = http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(payloadBytes))
		if err != nil {
			return fmt.Errorf("failed to create webhook request: %v", err)
		}

		req.Header.Set("Content-Type", "application/json")

		if secret != "" {
			signature := generateSignature(secret, payloadBytes)
			req.Header.Set("X-Webhook-Signature", signature)
		}

		client := d.Client()
		resp, err = client.Do(req)
		if err != nil {
			return fmt.Errorf("failed to send webhook request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("webhook request failed with status code: %d", resp.StatusCode)
		}
	}

	return nil
}
