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
