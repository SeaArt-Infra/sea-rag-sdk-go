# sea-rag-sdk-go

Go SDK for SeaArt RAG. It wraps the RAGFlow REST API through the SeaArt gateway, with helpers for dataset ingestion, document and chunk management, retrieval, and chat completion.

## Available Resources

| Resource | Client field | Function |
| --- | --- | --- |
| Datasets | `client.Datasets` | Create, list, get, update, and delete datasets |
| Documents | `client.Documents` | Upload, list, update, parse, stop, delete, and download documents |
| Chunks | `client.Chunks` | Start or cancel parsing and manage document chunks |
| Retrieval | `client.Retrieval` | Retrieve grounded chunks from one or more datasets |
| Chat | `client.Chat` | Manage chat assistants and run JSON or streaming completions |
| Raw | `client.Raw` | Call an RAGFlow API that does not yet have a typed helper |

## Gateway Routing

Pass the SeaArt gateway base URL only. The SDK appends `/rag` exactly once, then every resource appends the RAGFlow API prefix `/api/v1`.

| Input endpoint | Request example |
| --- | --- |
| `https://gateway.example.com` | `https://gateway.example.com/rag/api/v1/retrieval` |
| `https://gateway.example.com/rag` | `https://gateway.example.com/rag/api/v1/retrieval` |
| `https://gateway.example.com/team-a` | `https://gateway.example.com/team-a/rag/api/v1/retrieval` |

This matches OpenResty's `/rag/` route, which removes `/rag` before proxying to RAGFlow. Do not add `/api/v1` to `Endpoint`.

## Install

```bash
go get github.com/SeaArt-Infra/sea-rag-sdk-go
```

The module requires Go 1.24.3 or newer.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"os"

	searagsdk "github.com/SeaArt-Infra/sea-rag-sdk-go"
)

func main() {
	client := searagsdk.NewClient(searagsdk.ClientOptions{
		Endpoint: os.Getenv("SEAART_GATEWAY_BASE_URL"),
		APIKey:   os.Getenv("SEAART_RAG_API_KEY"),
		Headers:  map[string]string{"X-Project-ID": os.Getenv("RAGFLOW_PROJECT_ID")},
	})

	result, err := client.Retrieval.Search(context.Background(), map[string]any{
		"dataset_ids": []string{"dataset-id"},
		"question":    "What does the handbook say about leave?",
		"top_k":       20,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%#v\n", result)
}
```

The SDK sends `Authorization: Bearer <APIKey>` unless `Headers` already supplies an Authorization header. This project-scoped RAGFlow deployment requires `Headers["X-Project-ID"]`; the SDK mirrors it into `project_id` in JSON and multipart request bodies. Conversely, a JSON or multipart `project_id` sends the same header. The header wins if both differ.

## Ingest A Document

Create a dataset, upload one or more files as RAGFlow's repeated `file` form field, then start parsing. Every JSON resource method returns `RAGResponse[T]`: the envelope keeps `Code`, `Message`, and `Success()`, while `Data` is a typed resource payload.

```go
ctx := context.Background()
datasetID := os.Getenv("RAGFLOW_DATASET_ID")

file, err := os.Open("handbook.pdf")
if err != nil {
	panic(err)
}
defer file.Close()

uploaded, err := client.Documents.Upload(ctx, datasetID, []searagsdk.UploadFile{{
	Name:   "handbook.pdf",
	Reader: file,
}})
if err != nil {
	panic(err)
}
if len(uploaded.Data) == 0 {
	panic("RAGFlow upload returned no document")
}
documentID := uploaded.Data[0].ID

_, err = client.Chunks.StartParsing(ctx, datasetID, []string{documentID})
if err != nil {
	panic(err)
}

_, err = client.Documents.WaitForParsed(ctx, datasetID, documentID, searagsdk.WaitForParsedOptions{})
if err != nil {
	panic(err)
}
```

Use `client.Documents.Parse` and `client.Documents.Stop` for the newer document parse endpoints. Use `client.Chunks.StartParsing` and `client.Chunks.CancelParsing` for the RAGFlow-compatible chunk parse endpoints.

Parsing is asynchronous. Call `client.Documents.WaitForParsed` before retrieval; it polls the uploaded document every second by default and waits for up to 15 minutes. It returns a typed `Document` on `DONE`, returns `*searagsdk.ParsingFailedError` on `CANCEL` or `FAIL`, and returns `*searagsdk.ParsingTimeoutError` on timeout. `WaitForParsedOptions.OnProgress` receives every observed document state.

## Retrieve And Manage Chunks

`Retrieval.Search` accepts the RAGFlow retrieval body unchanged. It supports `dataset_ids`, `document_ids`, `question`, `similarity_threshold`, `vector_similarity_weight`, `top_k`, `rerank_id`, metadata filters, and graph retrieval options.

```go
result, err := client.Retrieval.Search(ctx, map[string]any{
	"dataset_ids":               []string{"dataset-id"},
	"question":                  "How are expenses approved?",
	"similarity_threshold":      0.2,
	"vector_similarity_weight": 0.3,
	"top_k":                     20,
	"highlight":                 true,
})
```

Use `client.Chunks.List`, `Get`, `Create`, `Update`, and `Delete` for manual curation. Identifiers are URL-escaped by the SDK.

## Chat Completion And Streaming

Use `client.Chat.Complete` with an RAGFlow chat-completion body. `client.Chat.Stream` adds `stream: true` and forwards UTF-8-safe raw SSE chunks to the callback. The SDK intentionally leaves SSE event parsing to the application because RAGFlow event payloads can vary with the selected chat configuration.

```go
err := client.Chat.Stream(ctx, map[string]any{
	"chat_id": "chat-assistant-id",
	"question": "Summarize the leave policy.",
}, func(chunk string) {
	fmt.Print(chunk)
})
```

## Errors And Unsupported APIs

HTTP failures and RAGFlow envelopes with a non-zero `code` return `*searagsdk.APIError`. Use `errors.As` to inspect `StatusCode`, RAGFlow `Code`, and `Message`. `WaitForParsed` additionally returns `*searagsdk.ParsingFailedError` or `*searagsdk.ParsingTimeoutError` for terminal parse outcomes.

For an API not represented by the core resources, call `client.Raw.Request` with a full RAGFlow path:

```go
models, err := client.Raw.Request(ctx, "GET", "/api/v1/models", nil, nil)
```

## Verify

```bash
go test ./...
```

Never expose API keys in browser code, source control, logs, or telemetry. Redact customer documents and retrieved chunks from diagnostic output.

<script
  type="text/plain"
  data-doc-skill
  data-doc-skill-id="sea-rag-sdk-go"
  data-doc-skill-label="SeaRAG Go SDK"
  data-doc-skill-filename="sea-rag-sdk-go-SKILL.md"
  data-doc-skill-version="1"
>
---
name: sea-rag-sdk-go
description: Integrate Go services with SeaArt RAG and RAGFlow through the official sea-rag-sdk-go. Use for dataset creation, document upload and parsing, parsing-status waits, chunk management, retrieval, chat completion, or RAGFlow API access from Go.
---

# SeaRAG Go SDK

Use `github.com/SeaArt-Infra/sea-rag-sdk-go` instead of hand-written RAGFlow HTTP calls.

## Workflow

1. Add the module with `go get github.com/SeaArt-Infra/sea-rag-sdk-go`.
2. Create one `searagsdk.Client` with the SeaArt gateway base URL and API key.
3. Pass the gateway base URL only. The SDK appends `/rag` once and resource methods add `/api/v1`.
4. Use typed `RAGResponse[T]` values from resource methods; read the typed payload from `response.Data`.
5. Use `WaitForParsed` after starting parsing, before retrieval.
6. Run `go test ./...` after changing the integration.

The client sends `Authorization: Bearer <APIKey>` unless `ClientOptions.Headers` already supplies Authorization. This project-scoped RAGFlow deployment requires `Headers["X-Project-ID"]`; the SDK mirrors it into `project_id` in JSON and multipart request bodies. Conversely, a JSON or multipart `project_id` sends the same header. The header wins if both differ. Do not include `/rag` or `/api/v1` in normal endpoint configuration.

## Shortest Runnable Flow

Set `SEAART_GATEWAY_BASE_URL`, `SEAART_RAG_API_KEY`, `RAGFLOW_PROJECT_ID`, and `RAGFLOW_DATASET_ID`, then run this beside `handbook.pdf`:

```go
package main

import (
	"context"
	"fmt"
	"os"

	searagsdk "github.com/SeaArt-Infra/sea-rag-sdk-go"
)

func main() {
	ctx := context.Background()
	datasetID := os.Getenv("RAGFLOW_DATASET_ID")
	projectID := os.Getenv("RAGFLOW_PROJECT_ID")
	if datasetID == "" || projectID == "" {
		panic("RAGFLOW_DATASET_ID and RAGFLOW_PROJECT_ID are required")
	}
	client := searagsdk.NewClient(searagsdk.ClientOptions{
		Endpoint: os.Getenv("SEAART_GATEWAY_BASE_URL"),
		APIKey:   os.Getenv("SEAART_RAG_API_KEY"),
		Headers:  map[string]string{"X-Project-ID": projectID},
	})

	file, err := os.Open("handbook.pdf")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	uploaded, err := client.Documents.Upload(ctx, datasetID, []searagsdk.UploadFile{{
		Name: "handbook.pdf", Reader: file,
	}})
	if err != nil {
		panic(err)
	}
	if len(uploaded.Data) == 0 {
		panic("RAGFlow upload returned no document")
	}
	documentID := uploaded.Data[0].ID
	if _, err = client.Chunks.StartParsing(ctx, datasetID, []string{documentID}); err != nil {
		panic(err)
	}
	if _, err = client.Documents.WaitForParsed(ctx, datasetID, documentID, searagsdk.WaitForParsedOptions{
		OnProgress: func(document searagsdk.Document) {
			fmt.Printf("%s %.0f%%\n", document.Run, document.Progress*100)
		},
	}); err != nil {
		panic(err)
	}

	result, err := client.Retrieval.Search(ctx, map[string]any{
		"dataset_ids": []string{datasetID}, "document_ids": []string{documentID},
		"question": "What does the handbook say about leave?", "top_k": 5,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("retrieved %d chunks\n", len(result.Data.Chunks))
}
```

`WaitForParsed` polls every second by default for up to 15 minutes. It returns a typed `Document` on `DONE`, calls `OnProgress` for every observed document state, returns `*ParsingFailedError` for `CANCEL` or `FAIL`, and returns `*ParsingTimeoutError` on timeout. RAGFlow normalizes its state as `UNSTART`, `RUNNING`, `CANCEL`, `DONE`, or `FAIL`.

## Other Resources

Use `client.Documents.Parse` and `Stop` for newer document parse endpoints. Use `client.Chunks.CancelParsing` to cancel compatible chunk parsing. Use `client.Chunks.List`, `Get`, `Create`, `Update`, and `Delete` for curation; `client.Chat.Complete` or `Stream` for configured chat assistants; and `client.Raw.Request(ctx, method, "/api/v1/...", query, body)` for an uncovered RAGFlow API.

## Safety

`*searagsdk.APIError` represents HTTP failures and RAGFlow envelopes whose `code` is non-zero. Keep API keys, customer documents, prompts, and raw retrieval output out of source control and logs.
</script>
