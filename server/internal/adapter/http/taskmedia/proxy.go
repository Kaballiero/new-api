package taskmedia

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/i18n"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/http/httpguts"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/internal/tasks/transport"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

var errTaskMediaRequestRejected = errors.New("task media request rejected")

type Proxy struct {
	ResponseHeaderTimeout  time.Duration
	DataURLMaxEncodedBytes int
}

func NewProxy() *Proxy {
	return &Proxy{ResponseHeaderTimeout: 60 * time.Second, DataURLMaxEncodedBytes: 64 << 20}
}

func (p *Proxy) Serve(c *gin.Context, task domain.MediaTask, descriptor *domain.MediaRequest) error {
	if descriptor == nil {
		return &domain.MediaError{
			Status: http.StatusInternalServerError, Code: "artifact_plugin_error",
			MessageID: i18n.MsgTasksArtifactRequestMissing, Message: "Artifact content plugin returned no request",
		}
	}
	rawURL := strings.TrimSpace(descriptor.URL)
	if rawURL == "" {
		return &domain.MediaError{
			Status: http.StatusGone, Code: "artifact_gone",
			MessageID: i18n.MsgTasksArtifactGone, Message: "Artifact content is no longer available",
		}
	}
	if strings.HasPrefix(rawURL, "data:") {
		if len(rawURL) > p.DataURLMaxEncodedBytes {
			return &domain.MediaError{
				Status: http.StatusBadGateway, Code: "artifact_request_rejected",
				MessageID: i18n.MsgTasksArtifactRequestRejected, Message: "Artifact request was rejected", Cause: errTaskMediaRequestRejected,
			}
		}
		if err := p.writeVideoDataURL(c, rawURL); err != nil {
			return &domain.MediaError{
				Status: http.StatusBadGateway, Code: "artifact_upstream_error",
				MessageID: i18n.MsgTasksArtifactDecodeFailed, Message: "Failed to decode artifact content", Cause: err,
			}
		}
		return nil
	}
	if len(rawURL) > 64<<10 {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactRequestRejected, Message: "Artifact request was rejected", Cause: errTaskMediaRequestRejected,
		}
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL == nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") ||
		parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactRequestRejected, Message: "Artifact request was rejected", Cause: errTaskMediaRequestRejected,
		}
	}
	if domain.IsMediaFallbackLoop(rawURL, task.TaskID) || isSelfTaskMediaURL(c, parsedURL) {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactLoopRejected, Message: "Artifact proxy loop was rejected", Cause: errTaskMediaRequestRejected,
		}
	}

	method := strings.ToUpper(strings.TrimSpace(descriptor.Method))
	if method == "" {
		method = c.Request.Method
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost:
	default:
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactMethodRejected, Message: "Artifact request method was rejected", Cause: errTaskMediaRequestRejected,
		}
	}
	if len(descriptor.Body) > 1<<20 {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactBodyRejected, Message: "Artifact request body was rejected", Cause: errTaskMediaRequestRejected,
		}
	}
	if descriptor.Credentialless &&
		(method != http.MethodGet && method != http.MethodHead ||
			descriptor.Body != nil || len(descriptor.Headers) != 0) {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactPublicRequestRejected, Message: "Credentialless artifact request was rejected", Cause: errTaskMediaRequestRejected,
		}
	}

	channel, err := model.CacheGetChannel(task.ChannelID)
	if err != nil {
		return &domain.MediaError{
			Status: http.StatusServiceUnavailable, Code: "artifact_plugin_unavailable",
			MessageID: i18n.MsgTasksArtifactChannelUnavailable, Message: "Artifact channel is unavailable", Cause: err,
		}
	}
	proxy := strings.TrimSpace(channel.GetSetting().Proxy)
	if err := validateTaskMediaURL(rawURL, proxy); err != nil {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactRequestRejected, Message: "Artifact request was rejected", Cause: err,
		}
	}

	client := service.GetSSRFProtectedHTTPClient()
	if proxy != "" {
		client, err = service.GetHttpClientWithProxy(proxy)
		if err != nil {
			return &domain.MediaError{
				Status: http.StatusInternalServerError, Code: "artifact_internal_error",
				MessageID: i18n.MsgTasksArtifactProxyClientFailed, Message: "Failed to create artifact proxy client", Cause: err,
			}
		}
	}
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), method, parsedURL.String(), bytes.NewReader(descriptor.Body))
	if err != nil {
		return &domain.MediaError{
			Status: http.StatusInternalServerError, Code: "artifact_internal_error",
			MessageID: i18n.MsgTasksArtifactRequestFailed, Message: "Failed to create artifact request", Cause: err,
		}
	}
	if err := applyTaskMediaRequestHeaders(req.Header, descriptor.Headers); err != nil {
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_request_rejected",
			MessageID: i18n.MsgTasksArtifactHeadersRejected, Message: "Artifact request headers were rejected", Cause: err,
		}
	}
	clientHeaders := transport.MediaClientHeaders(c.Request.Header)
	for name, value := range clientHeaders {
		req.Header.Set(name, value)
	}

	client = taskMediaRedirectClient(client, proxy, c, clientHeaders, descriptor.Credentialless)
	clientWithoutBodyTimeout := *client
	clientWithoutBodyTimeout.Timeout = 0
	resp, err := doTaskMediaRequest(&clientWithoutBodyTimeout, req, p.ResponseHeaderTimeout)
	if err != nil {
		if errors.Is(err, errTaskMediaRequestRejected) {
			return &domain.MediaError{
				Status: http.StatusBadGateway, Code: "artifact_request_rejected",
				MessageID: i18n.MsgTasksArtifactRedirectRejected, Message: "Artifact redirect was rejected", Cause: err,
			}
		}
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
			return &domain.MediaError{
				Status: http.StatusGatewayTimeout, Code: "artifact_upstream_timeout",
				MessageID: i18n.MsgTasksArtifactTimeout, Message: "Artifact upstream request timed out", Cause: err,
			}
		}
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_upstream_error",
			MessageID: i18n.MsgTasksArtifactFetchFailed, Message: "Failed to fetch artifact content", Cause: err,
		}
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified, http.StatusRequestedRangeNotSatisfiable:
		copyTaskMediaResponseHeaders(c.Writer.Header(), resp.Header)
		setTaskMediaResponseSecurityHeaders(c.Writer.Header())
		c.Status(resp.StatusCode)
		c.Writer.WriteHeaderNow()
		if c.Request.Method == http.MethodHead || resp.StatusCode == http.StatusNotModified {
			return nil
		}
		if _, err := io.Copy(c.Writer, resp.Body); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream task media: %v", err))
		}
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_upstream_auth_failed",
			MessageID: i18n.MsgTasksArtifactAuthenticationFailed, Message: "Artifact upstream authentication failed",
		}
	case http.StatusNotFound, http.StatusGone:
		return &domain.MediaError{
			Status: http.StatusGone, Code: "artifact_gone",
			MessageID: i18n.MsgTasksArtifactGone, Message: "Artifact content is no longer available",
		}
	case http.StatusTooManyRequests:
		if retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After")); retryAfter != "" &&
			len(retryAfter) <= 256 && !strings.ContainsAny(retryAfter, "\r\n") {
			c.Header("Retry-After", retryAfter)
		}
		return &domain.MediaError{
			Status: http.StatusServiceUnavailable, Code: "artifact_upstream_busy",
			MessageID: i18n.MsgTasksArtifactBusy, Message: "Artifact upstream is busy",
		}
	default:
		return &domain.MediaError{
			Status: http.StatusBadGateway, Code: "artifact_upstream_error",
			MessageID: i18n.MsgTasksArtifactUpstreamStatus, MessageData: map[string]any{"Status": resp.StatusCode}, Message: fmt.Sprintf("Artifact upstream returned status %d", resp.StatusCode),
		}
	}
}

type taskMediaHTTPResult struct {
	response *http.Response
	err      error
}

type taskMediaCancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
	once   sync.Once
}

func (b *taskMediaCancelBody) Close() error {
	b.once.Do(b.cancel)
	return b.ReadCloser.Close()
}

func doTaskMediaRequest(client *http.Client, request *http.Request, responseHeaderTimeout time.Duration) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	requestContext, cancel := context.WithCancel(request.Context())
	request = request.Clone(requestContext)
	resultChannel := make(chan taskMediaHTTPResult, 1)
	go func() {
		response, err := client.Do(request)
		resultChannel <- taskMediaHTTPResult{response: response, err: err}
	}()

	timer := time.NewTimer(responseHeaderTimeout)
	defer timer.Stop()
	cleanupResult := func() {
		go func() {
			result := <-resultChannel
			if result.response != nil && result.response.Body != nil {
				_ = result.response.Body.Close()
			}
		}()
	}

	select {
	case result := <-resultChannel:
		if result.err != nil {
			cancel()
			if result.response != nil && result.response.Body != nil {
				_ = result.response.Body.Close()
			}
			return nil, result.err
		}
		if result.response == nil || result.response.Body == nil {
			cancel()
			return nil, errors.New("artifact upstream returned no response body")
		}
		result.response.Body = &taskMediaCancelBody{
			ReadCloser: result.response.Body,
			cancel:     cancel,
		}
		return result.response, nil
	case <-timer.C:
		cancel()
		cleanupResult()
		return nil, context.DeadlineExceeded
	case <-request.Context().Done():
		cancel()
		cleanupResult()
		return nil, request.Context().Err()
	}
}

func applyTaskMediaRequestHeaders(destination http.Header, headers map[string]string) error {
	if len(headers) > 64 {
		return errTaskMediaRequestRejected
	}
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) || len(value) > 8192 {
			return errTaskMediaRequestRejected
		}
		switch strings.ToLower(name) {
		case "host", "content-length", "accept-encoding", "connection", "proxy-connection", "keep-alive",
			"proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
			return errTaskMediaRequestRejected
		}
		destination.Set(name, value)
	}
	return nil
}

func taskMediaRedirectClient(base *http.Client, proxy string, c *gin.Context, clientHeaders map[string]string, credentialless bool) *http.Client {
	cloned := *base
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("%w: too many redirects", errTaskMediaRequestRejected)
		}
		if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") ||
			req.URL.Host == "" || req.URL.User != nil || req.URL.Fragment != "" {
			return fmt.Errorf("%w: invalid redirect URL", errTaskMediaRequestRejected)
		}
		if err := validateTaskMediaURL(req.URL.String(), proxy); err != nil {
			return fmt.Errorf("%w: %v", errTaskMediaRequestRejected, err)
		}
		if isSelfTaskMediaURL(c, req.URL) {
			return fmt.Errorf("%w: proxy loop", errTaskMediaRequestRejected)
		}
		if len(via) > 0 && !sameTaskMediaOrigin(via[len(via)-1].URL, req.URL) {
			if !credentialless {
				return fmt.Errorf("%w: credentialed cross-origin redirect", errTaskMediaRequestRejected)
			}
			for name := range req.Header {
				req.Header.Del(name)
			}
			req.Body = http.NoBody
			req.GetBody = nil
			req.ContentLength = 0
		}
		for name, value := range clientHeaders {
			req.Header.Set(name, value)
		}
		return nil
	}
	return &cloned
}

func validateTaskMediaURL(rawURL, proxy string) error {
	if proxy == "" {
		return service.ValidateSSRFProtectedFetchURL(rawURL)
	}
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(
		rawURL,
		fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	)
}

func sameTaskMediaOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(normalizeTaskMediaHost(left.Scheme, left.Host), normalizeTaskMediaHost(right.Scheme, right.Host))
}

func normalizeTaskMediaHost(scheme, host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	hostname, port, err := net.SplitHostPort(host)
	if err != nil {
		return strings.TrimSuffix(host, ".")
	}
	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	if (strings.EqualFold(scheme, "http") && port == "80") || (strings.EqualFold(scheme, "https") && port == "443") {
		return hostname
	}
	return net.JoinHostPort(hostname, port)
}

func isSelfTaskMediaURL(c *gin.Context, target *url.URL) bool {
	if c == nil || target == nil || !isTaskMediaProxyPath(target.Path) {
		return false
	}
	targetHost := normalizeTaskMediaHost(target.Scheme, target.Host)
	if targetHost == "" {
		return true
	}
	scheme := strings.TrimSpace(strings.Split(c.Request.Header.Get("X-Forwarded-Proto"), ",")[0])
	if scheme == "" {
		scheme = "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
	}
	hosts := []string{c.Request.Host}
	if forwardedHost := strings.TrimSpace(strings.Split(c.Request.Header.Get("X-Forwarded-Host"), ",")[0]); forwardedHost != "" {
		hosts = append(hosts, forwardedHost)
	}
	for _, host := range hosts {
		if strings.EqualFold(targetHost, normalizeTaskMediaHost(scheme, host)) {
			return true
		}
	}
	return false
}

func isTaskMediaProxyPath(path string) bool {
	if strings.HasPrefix(path, "/v1/videos/") && strings.HasSuffix(path, "/content") {
		return true
	}
	return strings.HasPrefix(path, "/v1/tasks/") &&
		strings.Contains(path, "/artifacts/") &&
		strings.HasSuffix(path, "/content")
}

func copyTaskMediaResponseHeaders(destination, source http.Header) {
	for _, name := range []string{
		"Content-Type",
		"Content-Length",
		"Content-Range",
		"Accept-Ranges",
		"ETag",
		"Last-Modified",
		"Content-Disposition",
	} {
		for _, value := range source.Values(name) {
			destination.Add(name, value)
		}
	}
}

func setTaskMediaResponseSecurityHeaders(header http.Header) {
	header.Set("Cache-Control", "private, no-store")
	header.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
}

func (p *Proxy) writeVideoDataURL(c *gin.Context, dataURL string) error {
	if len(dataURL) > p.DataURLMaxEncodedBytes {
		return errTaskMediaRequestRejected
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}
	if len(mimeType) > 255 || !httpguts.ValidHeaderFieldValue(mimeType) {
		return fmt.Errorf("invalid data url media type")
	}

	var encoding *base64.Encoding
	var contentLength int64
	for _, candidate := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		decodedLength, err := io.Copy(io.Discard, base64.NewDecoder(candidate, strings.NewReader(payload)))
		if err == nil {
			encoding = candidate
			contentLength = decodedLength
			break
		}
	}
	if encoding == nil {
		return fmt.Errorf("invalid base64 data")
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Content-Length", strconv.FormatInt(contentLength, 10))
	setTaskMediaResponseSecurityHeaders(c.Writer.Header())
	c.Writer.WriteHeader(http.StatusOK)
	if c.Request.Method == http.MethodHead {
		return nil
	}
	_, err := io.Copy(c.Writer, base64.NewDecoder(encoding, strings.NewReader(payload)))
	return err
}
