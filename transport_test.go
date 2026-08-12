package searagsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeRAGEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		want     string
	}{
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080/rag"},
		{"http://127.0.0.1:8080/", "http://127.0.0.1:8080/rag"},
		{"http://127.0.0.1:8080/rag", "http://127.0.0.1:8080/rag"},
		{"http://127.0.0.1:8080/rag/", "http://127.0.0.1:8080/rag/"},
		{"https://example.com/api?debug=1", "https://example.com/api/rag?debug=1"},
		{"", ""},
	}
	for _, test := range tests {
		if got := NormalizeRAGEndpoint(test.endpoint); got != test.want {
			t.Fatalf("NormalizeRAGEndpoint(%q) = %q, want %q", test.endpoint, got, test.want)
		}
	}
}

func TestBuildURLAndHeaders(t *testing.T) {
	transport := NewTransport("https://example.com/base?debug=1", "secret", map[string]string{"X-User-ID": "user_1"}, nil)
	got, err := transport.BuildURL("/api/v1/datasets/a%2Fb", QueryParams{"ids": []string{"one", "two"}, "desc": false, "page": 0})
	if err != nil {
		t.Fatal(err)
	}
	want := "https://example.com/base/rag/api/v1/datasets/a%2Fb?debug=1&desc=false&ids=one&ids=two"
	if got != want {
		t.Fatalf("BuildURL() = %q, want %q", got, want)
	}
	headers := transport.BuildHeaders("application/json", true, map[string]string{"authorization": "Bearer custom"})
	if headers["X-User-ID"] != "user_1" || headers["authorization"] != "Bearer custom" {
		t.Fatalf("unexpected headers: %#v", headers)
	}
	if _, exists := headers["Authorization"]; exists {
		t.Fatalf("authorization should not have been duplicated: %#v", headers)
	}
}

func TestProjectHeaderIsSentInHeaderAndJSONBody(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedHeaders = request.Header.Clone()
		if err := json.NewDecoder(request.Body).Decode(&receivedBody); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":{"chunks":[]}}`))
	}))
	defer server.Close()

	payload := map[string]any{"question": "hello"}
	client := NewClient(ClientOptions{
		Endpoint: server.URL,
		Headers:  map[string]string{projectIDHeader: "project_1"},
	})
	if _, err := client.Retrieval.Search(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if got := receivedHeaders.Get(projectIDHeader); got != "project_1" {
		t.Fatalf("%s = %q, want project_1", projectIDHeader, got)
	}
	if got := receivedBody["project_id"]; got != "project_1" {
		t.Fatalf("project_id = %#v, want project_1", got)
	}
	if _, exists := payload["project_id"]; exists {
		t.Fatalf("payload was mutated: %#v", payload)
	}
}

func TestProjectIDInJSONBodyAddsHeader(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedHeaders = request.Header.Clone()
		if err := json.NewDecoder(request.Body).Decode(&receivedBody); err != nil {
			t.Fatal(err)
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":{"chunks":[]}}`))
	}))
	defer server.Close()

	payload := map[string]any{"question": "hello", "project_id": "project_1"}
	client := NewClient(ClientOptions{Endpoint: server.URL})
	if _, err := client.Retrieval.Search(context.Background(), payload); err != nil {
		t.Fatal(err)
	}
	if got := receivedHeaders.Get(projectIDHeader); got != "project_1" {
		t.Fatalf("%s = %q, want project_1", projectIDHeader, got)
	}
	if got := receivedBody["project_id"]; got != "project_1" {
		t.Fatalf("project_id = %#v, want project_1", got)
	}
}

func TestExistingProjectHeaderIsAddedToMultipartBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get(projectIDHeader); got != "legacy-project" {
			t.Fatalf("%s = %q, want legacy-project", projectIDHeader, got)
		}
		if err := request.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		if got := request.FormValue("project_id"); got != "legacy-project" {
			t.Fatalf("project_id = %q, want legacy-project", got)
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		Endpoint: server.URL,
		Headers:  map[string]string{"x-project-id": "legacy-project"},
	})
	if _, err := client.Documents.Upload(context.Background(), "kb_1", []UploadFile{{
		Name: "notes.txt", Reader: strings.NewReader("rag content"),
	}}); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultHTTPClientsUseExpectedTimeouts(t *testing.T) {
	transport := NewTransport("http://127.0.0.1:8080", "", nil, nil)
	if transport.httpClient.Timeout != 180*time.Second {
		t.Fatalf("HTTP timeout = %s, want 180s", transport.httpClient.Timeout)
	}
	if transport.streamHTTPClient.Timeout != 0 {
		t.Fatalf("stream timeout = %s, want no total timeout", transport.streamHTTPClient.Timeout)
	}
}

func TestCompleteUTF8PrefixKeepsTrailingPartialRune(t *testing.T) {
	complete, pending := completeUTF8Prefix([]byte("data: \xe4"))
	if string(complete) != "data: " {
		t.Fatalf("complete = %q", complete)
	}
	if string(pending) != "\xe4" {
		t.Fatalf("pending = %q", pending)
	}
	complete, pending = completeUTF8Prefix(append(pending, []byte("\xbd\xa0\n\n")...))
	if string(complete) != "你\n\n" || len(pending) != 0 {
		t.Fatalf("complete = %q pending = %q", complete, pending)
	}
}

func TestDocumentUploadUsesGatewayRouteAndMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rag/api/v1/datasets/kb_1/documents" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer api-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if err := request.ParseMultipartForm(1024 * 1024); err != nil {
			t.Fatal(err)
		}
		files := request.MultipartForm.File["file"]
		if len(files) != 1 || files[0].Filename != "notes.txt" {
			t.Fatalf("files = %#v", files)
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		content, _ := io.ReadAll(file)
		if string(content) != "rag content" {
			t.Fatalf("content = %q", content)
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{Endpoint: server.URL, APIKey: "api-key"})
	_, err := client.Documents.Upload(context.Background(), "kb_1", []UploadFile{{Name: "notes.txt", Reader: strings.NewReader("rag content")}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocumentUploadDecodesNormalizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":0,"data":[{"id":"doc_1","name":"notes.txt","run":0}]}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{Endpoint: server.URL})
	response, err := client.Documents.Upload(context.Background(), "kb_1", []UploadFile{{Name: "notes.txt", Reader: strings.NewReader("rag content")}})
	if err != nil {
		t.Fatal(err)
	}
	if !response.Success() || len(response.Data) != 1 || response.Data[0].ID != "doc_1" {
		t.Fatalf("response = %#v", response)
	}
	if response.Data[0].Run != ParsingStatusUnstarted {
		t.Fatalf("run = %q", response.Data[0].Run)
	}
}

func TestWaitForParsedNormalizesTerminalStates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rag/api/v1/datasets/kb_1/documents" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		if request.URL.Query().Get("id") != "doc_1" {
			t.Fatalf("id = %q", request.URL.Query().Get("id"))
		}
		requests++
		run := "1"
		if requests == 2 {
			run = "DONE"
		}
		_, _ = writer.Write([]byte(`{"code":0,"data":{"docs":[{"id":"doc_1","run":"` + run + `"}]}}`))
	}))
	defer server.Close()

	var progress []ParsingStatus
	client := NewClient(ClientOptions{Endpoint: server.URL})
	document, err := client.Documents.WaitForParsed(context.Background(), "kb_1", "doc_1", WaitForParsedOptions{
		PollInterval: time.Millisecond,
		Timeout:      time.Second,
		OnProgress: func(document Document) {
			progress = append(progress, document.Run)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if document.Run != ParsingStatusDone {
		t.Fatalf("run = %q", document.Run)
	}
	if got, want := fmt.Sprint(progress), fmt.Sprint([]ParsingStatus{ParsingStatusRunning, ParsingStatusDone}); got != want {
		t.Fatalf("progress = %s, want %s", got, want)
	}
}

func TestWaitForParsedReturnsParsingFailedError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":0,"data":{"docs":[{"id":"doc_1","run":"FAIL","progress_msg":"invalid file"}]}}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{Endpoint: server.URL})
	_, err := client.Documents.WaitForParsed(context.Background(), "kb_1", "doc_1", WaitForParsedOptions{})
	var parsingError *ParsingFailedError
	if !errors.As(err, &parsingError) || parsingError.Document.Run != ParsingStatusFailed {
		t.Fatalf("error = %#v", err)
	}
}

func TestWaitForParsedReturnsParsingTimeoutError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":0,"data":{"docs":[{"id":"doc_1","run":"RUNNING"}]}}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{Endpoint: server.URL})
	document, err := client.Documents.WaitForParsed(context.Background(), "kb_1", "doc_1", WaitForParsedOptions{
		PollInterval: time.Millisecond,
		Timeout:      2 * time.Millisecond,
	})
	var timeoutError *ParsingTimeoutError
	if !errors.As(err, &timeoutError) {
		t.Fatalf("error = %#v", err)
	}
	if document.Run != ParsingStatusRunning || timeoutError.LastDocument == nil {
		t.Fatalf("document = %#v timeout = %#v", document, timeoutError)
	}
}

func TestNonZeroRAGCodeReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"code":102,"message":"dataset_ids is required"}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{Endpoint: server.URL})
	_, err := client.Retrieval.Search(context.Background(), map[string]any{})
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError.Code != 102 {
		t.Fatalf("error = %#v", err)
	}
}

func TestChatStreamSetsStreamAndDecodesUTF8(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rag/api/v1/chat/completions" {
			t.Fatalf("path = %s", request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["stream"] != true {
			t.Fatalf("stream = %#v", body["stream"])
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = writer.Write([]byte("data: "))
		_, _ = writer.Write([]byte("\xe4"))
		_, _ = writer.Write([]byte("\xbd\xa0\n\n"))
	}))
	defer server.Close()

	var chunks []string
	client := NewClient(ClientOptions{Endpoint: server.URL})
	err := client.Chat.Stream(context.Background(), map[string]any{"chat_id": "chat_1", "question": "hello"}, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chunks, ""); got != "data: 你\n\n" {
		t.Fatalf("stream = %q", got)
	}
}
