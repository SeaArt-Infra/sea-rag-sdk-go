package searagsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const defaultHTTPTimeout = 180 * time.Second

// Transport is the shared HTTP implementation used by each resource.
type Transport struct {
	endpoint         string
	apiKey           string
	headers          map[string]string
	httpClient       *http.Client
	streamHTTPClient *http.Client
}

// APIError reports an HTTP failure or a non-zero RAGFlow response code.
type APIError struct {
	StatusCode int
	Code       int
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("RAG API error %d: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func NewTransport(endpoint, apiKey string, headers map[string]string, httpClient *http.Client) *Transport {
	copyHeaders := make(map[string]string, len(headers))
	for key, value := range headers {
		copyHeaders[key] = value
	}

	if httpClient != nil {
		return &Transport{
			endpoint:         NormalizeRAGEndpoint(endpoint),
			apiKey:           apiKey,
			headers:          copyHeaders,
			httpClient:       httpClient,
			streamHTTPClient: httpClient,
		}
	}

	return &Transport{
		endpoint:         NormalizeRAGEndpoint(endpoint),
		apiKey:           apiKey,
		headers:          copyHeaders,
		httpClient:       &http.Client{Timeout: defaultHTTPTimeout},
		streamHTTPClient: &http.Client{},
	}
}

func (t *Transport) GetJSON(ctx context.Context, path string, query QueryParams, out any) error {
	return t.requestJSON(ctx, t.httpClient, http.MethodGet, path, query, nil, nil, out)
}

func (t *Transport) PostJSON(ctx context.Context, path string, body any, out any) error {
	return t.requestJSON(ctx, t.httpClient, http.MethodPost, path, nil, body, nil, out)
}

func (t *Transport) PutJSON(ctx context.Context, path string, body any, out any) error {
	return t.requestJSON(ctx, t.httpClient, http.MethodPut, path, nil, body, nil, out)
}

func (t *Transport) PatchJSON(ctx context.Context, path string, body any, out any) error {
	return t.requestJSON(ctx, t.httpClient, http.MethodPatch, path, nil, body, nil, out)
}

func (t *Transport) DeleteJSON(ctx context.Context, path string, body any, out any) error {
	return t.requestJSON(ctx, t.httpClient, http.MethodDelete, path, nil, body, nil, out)
}

func (t *Transport) RequestJSON(ctx context.Context, method, path string, query QueryParams, body any, out any) error {
	return t.requestJSON(ctx, t.httpClient, method, path, query, body, nil, out)
}

func (t *Transport) PostMultipart(ctx context.Context, path string, fields map[string]string, files []UploadFile, out any) error {
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return err
		}
	}
	for _, file := range files {
		if file.Name == "" || file.Reader == nil {
			return errors.New("each upload file requires a name and reader")
		}
		part, err := writer.CreateFormFile("file", file.Name)
		if err != nil {
			return err
		}
		if _, err := io.Copy(part, file.Reader); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	return t.requestJSON(ctx, t.httpClient, http.MethodPost, path, nil, &payload, map[string]string{
		"Content-Type": writer.FormDataContentType(),
	}, out)
}

// PostStream sends an SSE-compatible request and forwards raw response chunks.
func (t *Transport) PostStream(ctx context.Context, path string, body any, onChunk func(string)) error {
	if onChunk == nil {
		return errors.New("stream callback is required")
	}
	request, err := t.newRequest(ctx, http.MethodPost, path, nil, body, "text/event-stream", nil)
	if err != nil {
		return err
	}
	response, err := t.streamHTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("stream request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		raw, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return readErr
		}
		return httpError(response.StatusCode, string(raw))
	}

	buffer := make([]byte, 4096)
	pending := make([]byte, 0, 4)
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			pending = append(pending, buffer[:count]...)
			complete, remainder := completeUTF8Prefix(pending)
			if len(complete) > 0 {
				onChunk(string(complete))
			}
			pending = remainder
		}
		if errors.Is(readErr, io.EOF) {
			if len(pending) > 0 {
				onChunk(string(pending))
			}
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("stream failed: %w", readErr)
		}
	}
}

func (t *Transport) Download(ctx context.Context, path string) ([]byte, http.Header, error) {
	request, err := t.newRequest(ctx, http.MethodGet, path, nil, nil, "*/*", nil)
	if err != nil {
		return nil, nil, err
	}
	response, err := t.httpClient.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.Header, err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return nil, response.Header, httpError(response.StatusCode, string(raw))
	}
	return raw, response.Header, nil
}

func (t *Transport) requestJSON(ctx context.Context, client *http.Client, method, path string, query QueryParams, body any, extraHeaders map[string]string, out any) error {
	request, err := t.newRequest(ctx, method, path, query, body, "application/json", extraHeaders)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode >= http.StatusBadRequest {
		return httpError(response.StatusCode, string(raw))
	}
	if len(raw) == 0 || out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("expected JSON response, got: %s", responsePreview(raw))
	}
	return ragResponseError(raw)
}

func (t *Transport) newRequest(ctx context.Context, method, path string, query QueryParams, body any, accept string, extraHeaders map[string]string) (*http.Request, error) {
	urlValue, err := t.BuildURL(path, query)
	if err != nil {
		return nil, err
	}

	var reader io.Reader
	hasBody := body != nil
	if body != nil {
		if bodyReader, ok := body.(io.Reader); ok {
			reader = bodyReader
		} else {
			raw, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				return nil, marshalErr
			}
			reader = bytes.NewReader(raw)
		}
	}

	request, err := http.NewRequestWithContext(ctx, method, urlValue, reader)
	if err != nil {
		return nil, err
	}
	for key, value := range t.BuildHeaders(accept, hasBody, extraHeaders) {
		request.Header.Set(key, value)
	}
	if isDebugEnabled() {
		fmt.Fprintln(os.Stderr, method, urlValue)
	}
	return request, nil
}

func (t *Transport) BuildURL(path string, query QueryParams) (string, error) {
	parsed, err := url.Parse(t.endpoint)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(parsed.EscapedPath(), "/")
	relativePath := strings.TrimLeft(path, "/")
	rawPath := "/" + strings.TrimLeft(basePath+"/"+relativePath, "/")
	decodedPath, err := url.PathUnescape(rawPath)
	if err != nil {
		return "", err
	}
	parsed.Path = decodedPath
	parsed.RawPath = rawPath

	values := parsed.Query()
	for key, value := range query {
		setQueryValue(values, key, value)
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func (t *Transport) BuildHeaders(accept string, hasBody bool, requestHeaders map[string]string) map[string]string {
	headers := make(map[string]string, len(t.headers)+len(requestHeaders)+3)
	if accept != "" {
		headers["Accept"] = accept
	}
	if hasBody {
		headers["Content-Type"] = "application/json"
	}
	for key, value := range t.headers {
		if strings.TrimSpace(key) != "" {
			headers[key] = value
		}
	}
	for key, value := range requestHeaders {
		if strings.TrimSpace(key) != "" {
			headers[key] = value
		}
	}
	if t.apiKey != "" && !hasHeader(headers, "Authorization") {
		headers["Authorization"] = "Bearer " + t.apiKey
	}
	return headers
}

// NormalizeRAGEndpoint appends /rag once while preserving a base path and query.
func NormalizeRAGEndpoint(endpoint string) string {
	if strings.TrimSpace(endpoint) == "" {
		return endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	segments := make([]string, 0)
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	for _, segment := range segments {
		if segment == "rag" {
			return parsed.String()
		}
	}
	segments = append(segments, "rag")
	parsed.Path = "/" + strings.Join(segments, "/")
	parsed.RawPath = ""
	return parsed.String()
}

func setQueryValue(values url.Values, key string, value any) {
	if isZeroValue(value) {
		return
	}
	values.Del(key)
	reflectValue := reflect.ValueOf(value)
	if reflectValue.IsValid() && (reflectValue.Kind() == reflect.Slice || reflectValue.Kind() == reflect.Array) {
		for index := 0; index < reflectValue.Len(); index++ {
			item := reflectValue.Index(index).Interface()
			if !isZeroValue(item) {
				values.Add(key, fmt.Sprint(item))
			}
		}
		return
	}
	if boolValue, ok := value.(bool); ok {
		values.Set(key, strconv.FormatBool(boolValue))
		return
	}
	if boolValue, ok := value.(*bool); ok {
		values.Set(key, strconv.FormatBool(*boolValue))
		return
	}
	values.Set(key, fmt.Sprint(value))
}

func isZeroValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return typed == ""
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case float64:
		return typed == 0
	case bool:
		return false
	}
	reflectValue := reflect.ValueOf(value)
	return reflectValue.IsValid() && (reflectValue.Kind() == reflect.Pointer || reflectValue.Kind() == reflect.Interface) && reflectValue.IsNil()
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func httpError(statusCode int, body string) error {
	return &APIError{StatusCode: statusCode, Message: responseMessage(body)}
}

func ragResponseError(raw []byte) error {
	var response struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Code == nil || *response.Code == 0 {
		return nil
	}
	return &APIError{Code: *response.Code, Message: response.Message}
}

func responseMessage(body string) string {
	var response struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &response) == nil {
		if response.Error != "" {
			return response.Error
		}
		if response.Message != "" {
			return response.Message
		}
	}
	return body
}

func responsePreview(raw []byte) string {
	value := strings.Join(strings.Fields(string(raw)), " ")
	if len(value) > 240 {
		return value[:240]
	}
	return value
}

func urlEscape(value string) string {
	return url.PathEscape(value)
}

func isDebugEnabled() bool {
	return os.Getenv("SEARAG_DEBUG") == "1"
}

// completeUTF8Prefix separates a trailing, incomplete UTF-8 sequence from a
// response chunk so callbacks never receive a split valid rune.
func completeUTF8Prefix(raw []byte) ([]byte, []byte) {
	if utf8.Valid(raw) {
		return raw, nil
	}
	for index := len(raw) - 1; index >= 0; index-- {
		if utf8.Valid(raw[:index]) {
			return raw[:index], append([]byte(nil), raw[index:]...)
		}
	}
	return nil, append([]byte(nil), raw...)
}
