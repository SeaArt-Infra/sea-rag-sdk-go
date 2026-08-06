package searagsdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const apiPrefix = "/api/v1"

const (
	defaultParsePollInterval = time.Second
	defaultParseWaitTimeout  = 15 * time.Minute
)

type DatasetsResource struct{ transport *Transport }
type DocumentsResource struct{ transport *Transport }
type ChunksResource struct{ transport *Transport }
type RetrievalResource struct{ transport *Transport }
type ChatResource struct{ transport *Transport }
type RawResource struct{ transport *Transport }

// ParsingFailedError reports that RAGFlow ended parsing without indexing the document.
type ParsingFailedError struct {
	Document Document
}

func (e *ParsingFailedError) Error() string {
	message := strings.TrimSpace(e.Document.ProgressMsg)
	if message == "" {
		message = "RAGFlow parsing did not complete"
	}
	return fmt.Sprintf("document %q parsing ended as %s: %s", e.Document.ID, NormalizeParsingStatus(string(e.Document.Run)), message)
}

// ParsingTimeoutError reports that RAGFlow did not return a terminal parsing
// state before WaitForParsed's configured timeout.
type ParsingTimeoutError struct {
	DatasetID    string
	DocumentID   string
	Timeout      time.Duration
	LastDocument *Document
}

func (e *ParsingTimeoutError) Error() string {
	return fmt.Sprintf("document %q was not parsed within %s", e.DocumentID, e.Timeout)
}

func (r *DatasetsResource) Create(ctx context.Context, payload any) (RAGResponse[Dataset], error) {
	return requestRAG[Dataset](ctx, r.transport, http.MethodPost, apiPrefix+"/datasets", nil, payload)
}

func (r *DatasetsResource) List(ctx context.Context, options DatasetListOptions) (RAGResponse[[]Dataset], error) {
	return requestRAG[[]Dataset](ctx, r.transport, http.MethodGet, apiPrefix+"/datasets", QueryParams{
		"page":                   options.Page,
		"page_size":              options.PageSize,
		"orderby":                options.OrderBy,
		"desc":                   options.Desc,
		"id":                     options.ID,
		"name":                   options.Name,
		"include_parsing_status": options.IncludeParsingStatus,
	}, nil)
}

func (r *DatasetsResource) Get(ctx context.Context, datasetID string) (RAGResponse[Dataset], error) {
	return requestRAG[Dataset](ctx, r.transport, http.MethodGet, apiPrefix+"/datasets/"+urlEscape(datasetID), nil, nil)
}

func (r *DatasetsResource) Update(ctx context.Context, datasetID string, payload any) (RAGResponse[Dataset], error) {
	return requestRAG[Dataset](ctx, r.transport, http.MethodPut, apiPrefix+"/datasets/"+urlEscape(datasetID), nil, payload)
}

func (r *DatasetsResource) Delete(ctx context.Context, ids []string, deleteAll bool) (RAGResponse[json.RawMessage], error) {
	body := map[string]any{"delete_all": deleteAll}
	if len(ids) > 0 {
		body["ids"] = ids
	}
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodDelete, apiPrefix+"/datasets", nil, body)
}

func (r *DocumentsResource) Upload(ctx context.Context, datasetID string, files []UploadFile) (RAGResponse[[]Document], error) {
	return multipartRAG[[]Document](ctx, r.transport, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents", files)
}

func (r *DocumentsResource) List(ctx context.Context, datasetID string, options DocumentListOptions) (RAGResponse[DocumentList], error) {
	return requestRAG[DocumentList](ctx, r.transport, http.MethodGet, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents", QueryParams{
		"page":             options.Page,
		"page_size":        options.PageSize,
		"orderby":          options.OrderBy,
		"desc":             options.Desc,
		"id":               options.ID,
		"ids":              options.IDs,
		"name":             options.Name,
		"keywords":         options.Keywords,
		"create_time_from": options.CreateTimeFrom,
		"create_time_to":   options.CreateTimeTo,
		"suffix":           options.Suffix,
		"run":              options.Run,
	}, nil)
}

func (r *DocumentsResource) Update(ctx context.Context, datasetID, documentID string, payload any) (RAGResponse[Document], error) {
	return requestRAG[Document](ctx, r.transport, http.MethodPatch, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID), nil, payload)
}

func (r *DocumentsResource) Delete(ctx context.Context, datasetID string, ids []string, deleteAll bool) (RAGResponse[json.RawMessage], error) {
	body := map[string]any{"delete_all": deleteAll}
	if len(ids) > 0 {
		body["ids"] = ids
	}
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodDelete, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents", nil, body)
}

func (r *DocumentsResource) Parse(ctx context.Context, datasetID string, documentIDs []string) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPost, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/parse", nil, map[string]any{"document_ids": documentIDs})
}

func (r *DocumentsResource) Stop(ctx context.Context, datasetID string, documentIDs []string) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPost, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/stop", nil, map[string]any{"document_ids": documentIDs})
}

func (r *DocumentsResource) Download(ctx context.Context, datasetID, documentID string) ([]byte, http.Header, error) {
	return r.transport.Download(ctx, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID))
}

// WaitForParsed polls one document until RAGFlow reports DONE/3, fails it on
// CANCEL/2 or FAIL/4, and respects context cancellation and the configured timeout.
func (r *DocumentsResource) WaitForParsed(ctx context.Context, datasetID, documentID string, options WaitForParsedOptions) (Document, error) {
	if strings.TrimSpace(datasetID) == "" || strings.TrimSpace(documentID) == "" {
		return Document{}, errors.New("datasetID and documentID are required")
	}
	interval := options.PollInterval
	if interval <= 0 {
		interval = defaultParsePollInterval
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultParseWaitTimeout
	}
	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastDocument Document
	hasLastDocument := false

	for {
		response, err := r.List(pollCtx, datasetID, DocumentListOptions{ID: documentID, Page: 1, PageSize: 1})
		if err != nil {
			if errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
				return lastParsedDocument(lastDocument, hasLastDocument), &ParsingTimeoutError{
					DatasetID:    datasetID,
					DocumentID:   documentID,
					Timeout:      timeout,
					LastDocument: lastParsedDocumentPointer(lastDocument, hasLastDocument),
				}
			}
			return Document{}, err
		}
		if len(response.Data.Documents) > 0 {
			document := response.Data.Documents[0]
			status := NormalizeParsingStatus(string(document.Run))
			document.Run = status
			lastDocument = document
			hasLastDocument = true
			if options.OnProgress != nil {
				options.OnProgress(document)
			}
			switch status {
			case ParsingStatusDone:
				return document, nil
			case ParsingStatusCanceled, ParsingStatusFailed:
				return document, &ParsingFailedError{Document: document}
			}
		}

		if err := waitForNextParsePoll(pollCtx, interval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return lastParsedDocument(lastDocument, hasLastDocument), &ParsingTimeoutError{
					DatasetID:    datasetID,
					DocumentID:   documentID,
					Timeout:      timeout,
					LastDocument: lastParsedDocumentPointer(lastDocument, hasLastDocument),
				}
			}
			return Document{}, err
		}
	}
}

func (r *ChunksResource) StartParsing(ctx context.Context, datasetID string, documentIDs []string) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPost, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/chunks", nil, map[string]any{"document_ids": documentIDs})
}

func (r *ChunksResource) CancelParsing(ctx context.Context, datasetID string, documentIDs []string) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodDelete, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/chunks", nil, map[string]any{"document_ids": documentIDs})
}

func (r *ChunksResource) List(ctx context.Context, datasetID, documentID string, options ChunkListOptions) (RAGResponse[ChunkList], error) {
	return requestRAG[ChunkList](ctx, r.transport, http.MethodGet, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID)+"/chunks", QueryParams{
		"page":      options.Page,
		"page_size": options.PageSize,
		"id":        options.ID,
		"keywords":  options.Keywords,
	}, nil)
}

func (r *ChunksResource) Get(ctx context.Context, datasetID, documentID, chunkID string) (RAGResponse[Chunk], error) {
	return requestRAG[Chunk](ctx, r.transport, http.MethodGet, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID)+"/chunks/"+urlEscape(chunkID), nil, nil)
}

func (r *ChunksResource) Create(ctx context.Context, datasetID, documentID string, payload any) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPost, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID)+"/chunks", nil, payload)
}

func (r *ChunksResource) Update(ctx context.Context, datasetID, documentID, chunkID string, payload any) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPatch, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID)+"/chunks/"+urlEscape(chunkID), nil, payload)
}

func (r *ChunksResource) Delete(ctx context.Context, datasetID, documentID string, chunkIDs []string, deleteAll bool) (RAGResponse[json.RawMessage], error) {
	body := map[string]any{"delete_all": deleteAll}
	if len(chunkIDs) > 0 {
		body["chunk_ids"] = chunkIDs
	}
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodDelete, apiPrefix+"/datasets/"+urlEscape(datasetID)+"/documents/"+urlEscape(documentID)+"/chunks", nil, body)
}

func (r *RetrievalResource) Search(ctx context.Context, payload any) (RAGResponse[RetrievalResult], error) {
	return requestRAG[RetrievalResult](ctx, r.transport, http.MethodPost, apiPrefix+"/retrieval", nil, payload)
}

func (r *ChatResource) Create(ctx context.Context, payload any) (RAGResponse[Chat], error) {
	return requestRAG[Chat](ctx, r.transport, http.MethodPost, apiPrefix+"/chats", nil, payload)
}

func (r *ChatResource) List(ctx context.Context, options ChatListOptions) (RAGResponse[ChatList], error) {
	return requestRAG[ChatList](ctx, r.transport, http.MethodGet, apiPrefix+"/chats", QueryParams{
		"page":      options.Page,
		"page_size": options.PageSize,
		"orderby":   options.OrderBy,
		"desc":      options.Desc,
		"id":        options.ID,
		"name":      options.Name,
		"keywords":  options.Keywords,
	}, nil)
}

func (r *ChatResource) Get(ctx context.Context, chatID string) (RAGResponse[Chat], error) {
	return requestRAG[Chat](ctx, r.transport, http.MethodGet, apiPrefix+"/chats/"+urlEscape(chatID), nil, nil)
}

func (r *ChatResource) Update(ctx context.Context, chatID string, payload any) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPatch, apiPrefix+"/chats/"+urlEscape(chatID), nil, payload)
}

func (r *ChatResource) Delete(ctx context.Context, ids []string, deleteAll bool) (RAGResponse[json.RawMessage], error) {
	body := map[string]any{"delete_all": deleteAll}
	if len(ids) > 0 {
		body["ids"] = ids
	}
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodDelete, apiPrefix+"/chats", nil, body)
}

func (r *ChatResource) Complete(ctx context.Context, payload any) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, http.MethodPost, apiPrefix+"/chat/completions", nil, payload)
}

func (r *ChatResource) Stream(ctx context.Context, payload map[string]any, onChunk func(string)) error {
	body := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		body[key] = value
	}
	body["stream"] = true
	return r.transport.PostStream(ctx, apiPrefix+"/chat/completions", body, onChunk)
}

// Request provides access to RAGFlow routes that do not yet have a typed helper.
// Pass a full RAGFlow path such as /api/v1/agents when using this escape hatch.
func (r *RawResource) Request(ctx context.Context, method, path string, query QueryParams, body any) (RAGResponse[json.RawMessage], error) {
	return requestRAG[json.RawMessage](ctx, r.transport, method, path, query, body)
}

func requestRAG[T any](ctx context.Context, transport *Transport, method, path string, query QueryParams, body any) (RAGResponse[T], error) {
	var response RAGResponse[T]
	err := transport.RequestJSON(ctx, method, path, query, body, &response)
	return response, err
}

func multipartRAG[T any](ctx context.Context, transport *Transport, path string, files []UploadFile) (RAGResponse[T], error) {
	var response RAGResponse[T]
	err := transport.PostMultipart(ctx, path, nil, files, &response)
	return response, err
}

func waitForNextParsePoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func lastParsedDocument(document Document, exists bool) Document {
	if !exists {
		return Document{}
	}
	return document
}

func lastParsedDocumentPointer(document Document, exists bool) *Document {
	if !exists {
		return nil
	}
	return &document
}
