package upstream

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	jsonGetMaxAttempts = 2
	jsonGetRetryDelay  = 300 * time.Millisecond
	maxRetryAfter      = 5 * time.Second
)

// BrowserUserAgent 是所有上游 HTTP 请求统一使用的浏览器 User-Agent。
const BrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

type HTTPClient struct {
	client         *http.Client
	mu             sync.Mutex
	insecureClient *http.Client
}

type requestOptions struct {
	Context         context.Context
	Method          string
	Body            any
	Cookie          string
	UserID          string
	AccessToken     string
	TokenType       string
	InsecureSkipTLS bool
}

type jsonResponse struct {
	Payload any
	Header  http.Header
}

type textResponse struct {
	Body   string
	Header http.Header
	Status int
}

func NewHTTPClient(client *http.Client) *HTTPClient {
	return &HTTPClient{client: client}
}

func (c *HTTPClient) requestClient(skipTLS bool) *http.Client {
	if !skipTLS {
		return c.client
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.insecureClient != nil {
		return c.insecureClient
	}
	transport, ok := c.client.Transport.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	transport = transport.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	transport.TLSClientConfig.InsecureSkipVerify = true
	c.insecureClient = &http.Client{
		Transport:     transport,
		Timeout:       c.client.Timeout,
		CheckRedirect: c.client.CheckRedirect,
		Jar:           c.client.Jar,
	}
	return c.insecureClient
}

func (c *HTTPClient) requestJSON(reqURL string, options requestOptions) (jsonResponse, error) {
	method := options.Method
	if method == "" {
		method = http.MethodGet
	}
	options.Method = method
	attempts := 1
	if method == http.MethodGet {
		attempts = jsonGetMaxAttempts
	}

	for attempt := 1; attempt <= attempts; attempt++ {
		response, err := c.requestJSONOnce(reqURL, options)
		if attempt == attempts || !retryableJSONGet(method, err) {
			return response, err
		}
		delay := jsonGetRetryDelay
		if requestErr, ok := err.(*RequestError); ok && requestErr.RetryAfter > 0 {
			if requestErr.RetryAfter > maxRetryAfter {
				return response, err
			}
			if requestErr.RetryAfter > delay {
				delay = requestErr.RetryAfter
			}
		}
		ctx := options.Context
		if ctx == nil {
			ctx = context.Background()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return response, ctx.Err()
		case <-timer.C:
		}
	}
	return jsonResponse{}, newRequestError(ErrorUnknown, "")
}

func (c *HTTPClient) requestJSONOnce(reqURL string, options requestOptions) (jsonResponse, error) {
	method := options.Method

	body, err := encodeBody(options.Body)
	if err != nil {
		return jsonResponse{}, err
	}

	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return jsonResponse{}, newRequestError(ErrorInvalidURL, "")
	}
	req.Header.Set("Accept", "application/json")
	if options.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if options.AccessToken != "" {
		tokenType := options.TokenType
		if tokenType == "" {
			tokenType = "Bearer"
		}
		req.Header.Set("Authorization", tokenType+" "+options.AccessToken)
	}

	req.Header.Set("User-Agent", BrowserUserAgent)
	if options.Cookie != "" {
		req.Header.Set("Cookie", options.Cookie)
	}

	if options.UserID != "" {
		req.Header.Set("New-Api-User", options.UserID)
	}

	response, err := c.requestClient(options.InsecureSkipTLS).Do(req)
	if err != nil {
		log.Printf("[http-client] 请求失败 url=%s err=%v", reqURL, err)
		return jsonResponse{}, newRequestError(ErrorNetwork, "")
	}
	defer response.Body.Close()

	data, readErr := io.ReadAll(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		log.Printf("[http-client] 非 2xx 响应 url=%s status=%d", reqURL, response.StatusCode)
		var payload any
		if readErr == nil {
			_ = json.Unmarshal(data, &payload)
		}
		if record, ok := payload.(map[string]any); ok && record["reason"] == "LOGIN_AGREEMENT_REQUIRED" {
			requestErr := requestErrorForStatus(response.StatusCode, response.Header.Get("Retry-After"))
			requestErr.MessageKey = ErrorLoginAgreementRequired
			requestErr.Platform = PlatformSub2API
			return jsonResponse{}, requestErr
		}
		return jsonResponse{}, requestErrorForStatus(response.StatusCode, response.Header.Get("Retry-After"))
	}
	if readErr != nil {
		log.Printf("[http-client] 读取响应体失败 url=%s err=%v", reqURL, readErr)
		requestErr := newRequestError(ErrorNetwork, "")
		requestErr.HTTPStatus = response.StatusCode
		return jsonResponse{}, requestErr
	}
	payload, err := parseJSON(bytes.NewReader(data), reqURL)
	if err != nil {
		if requestErr, ok := err.(*RequestError); ok {
			requestErr.HTTPStatus = response.StatusCode
		}
		return jsonResponse{}, err
	}
	return jsonResponse{Payload: payload, Header: response.Header}, nil
}

func retryableJSONGet(method string, err error) bool {
	if method != http.MethodGet || err == nil {
		return false
	}
	requestErr, ok := err.(*RequestError)
	if !ok {
		return false
	}
	if requestErr.MessageKey == ErrorNetwork {
		return true
	}
	if requestErr.MessageKey == ErrorInvalidResponse && requestErr.HTTPStatus >= 200 && requestErr.HTTPStatus < 300 {
		return true
	}
	switch requestErr.HTTPStatus {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func requestErrorForStatus(status int, retryAfter string) *RequestError {
	key := ErrorRequest
	switch {
	case status == http.StatusUnauthorized:
		key = ErrorAuth
	case status == http.StatusForbidden:
		key = ErrorForbidden
	case status == http.StatusTooManyRequests:
		key = ErrorRateLimited
	case status == http.StatusRequestTimeout, status == http.StatusTooEarly, status >= 500:
		key = ErrorUnavailable
	}
	requestErr := newRequestError(key, "")
	requestErr.HTTPStatus = status
	requestErr.RetryAfter = parseRetryAfter(retryAfter)
	return requestErr
}

func parseRetryAfter(value string) time.Duration {
	trimmed := strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(trimmed, 10, 64); err == nil {
		if seconds > uint64(maxRetryAfter/time.Second) {
			return maxRetryAfter + time.Nanosecond
		}
		return time.Duration(seconds) * time.Second
	}
	if trimmed != "" && strings.Trim(trimmed, "0123456789") == "" {
		return maxRetryAfter + time.Nanosecond
	}
	if retryAt, err := http.ParseTime(trimmed); err == nil {
		if delay := time.Until(retryAt); delay > 0 {
			return delay
		}
	}
	return 0
}

func (c *HTTPClient) requestText(reqURL string, options requestOptions) (textResponse, error) {
	method := options.Method
	if method == "" {
		method = http.MethodGet
	}

	body, err := encodeBody(options.Body)
	if err != nil {
		return textResponse{}, err
	}

	ctx := options.Context
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return textResponse{}, newRequestError(ErrorInvalidURL, "")
	}
	req.Header.Set("Accept", "text/event-stream, application/json")
	if options.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if options.AccessToken != "" {
		tokenType := options.TokenType
		if tokenType == "" {
			tokenType = "Bearer"
		}
		req.Header.Set("Authorization", tokenType+" "+options.AccessToken)
	}
	req.Header.Set("User-Agent", BrowserUserAgent)
	if options.Cookie != "" {
		req.Header.Set("Cookie", options.Cookie)
	}
	if options.UserID != "" {
		req.Header.Set("New-Api-User", options.UserID)
	}

	response, err := c.requestClient(options.InsecureSkipTLS).Do(req)
	if err != nil {
		log.Printf("[http-client] 请求失败 url=%s err=%v", reqURL, err)
		return textResponse{}, newRequestError(ErrorNetwork, "")
	}
	defer response.Body.Close()

	data, err := io.ReadAll(response.Body)
	if err != nil {
		log.Printf("[http-client] 读取响应体失败 url=%s err=%v", reqURL, err)
		return textResponse{}, newRequestError(ErrorInvalidResponse, "")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		log.Printf("[http-client] 非 2xx 响应 url=%s status=%d", reqURL, response.StatusCode)
		return textResponse{}, requestErrorForStatus(response.StatusCode, response.Header.Get("Retry-After"))
	}
	return textResponse{Body: string(data), Header: response.Header, Status: response.StatusCode}, nil
}

func encodeBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, newRequestError(ErrorInvalidResponse, "")
	}
	return bytes.NewReader(encoded), nil
}

func parseJSON(reader io.Reader, reqURL string) (any, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		log.Printf("[http-client] 读取响应体失败 url=%s err=%v", reqURL, err)
		return nil, newRequestError(ErrorInvalidResponse, "")
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Printf("[http-client] JSON 解析失败 url=%s len=%d", reqURL, len(data))
		return nil, newRequestError(ErrorInvalidResponse, "")
	}
	return payload, nil
}
