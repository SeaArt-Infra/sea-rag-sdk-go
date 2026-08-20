package searagsdk

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// QueryParams contains URL query parameters. Values can be scalars or slices.
type QueryParams map[string]any

// ClientOptions configures a Client. Endpoint is the gateway base URL; /rag is
// appended automatically unless it is already present in the URL path.
type ClientOptions struct {
	Endpoint   string
	APIKey     string
	Headers    map[string]string
	HTTPClient *http.Client
}

// RAGResponse is RAGFlow's normalized response envelope.
// Data is typed by the resource method that produced the response.
type RAGResponse[T any] struct {
	Code          int    `json:"code"`
	Data          T      `json:"data"`
	Message       string `json:"message"`
	TotalDatasets int    `json:"total_datasets,omitempty"`
}

func (r RAGResponse[T]) Success() bool {
	return r.Code == 0
}

// ParsingStatus is RAGFlow's normalized document parsing state.
// RAGFlow normally returns the text names, while some compatible deployments
// return their numeric equivalents.
type ParsingStatus string

const (
	ParsingStatusUnstarted ParsingStatus = "UNSTART"
	ParsingStatusRunning   ParsingStatus = "RUNNING"
	ParsingStatusCanceled  ParsingStatus = "CANCEL"
	ParsingStatusDone      ParsingStatus = "DONE"
	ParsingStatusFailed    ParsingStatus = "FAIL"
	ParsingStatusScheduled ParsingStatus = "SCHEDULE"
)

// UploadFile is one file sent to RAGFlow's repeated multipart form field "file".
type UploadFile struct {
	Name   string
	Reader io.Reader
}

// UploadedFile is an attachment created by Documents.UploadInfoFromURL.
// It is not a dataset document and has not been parsed or indexed.
type UploadedFile struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Size       int64   `json:"size"`
	Extension  string  `json:"extension"`
	MIMEType   string  `json:"mime_type"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  float64 `json:"created_at"`
	PreviewURL string  `json:"preview_url"`
}

type Dataset struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	EmbeddingModel string `json:"embedding_model"`
	Permission     string `json:"permission"`
	ChunkMethod    string `json:"chunk_method"`
	DocumentCount  int    `json:"document_count"`
	ChunkCount     int    `json:"chunk_count"`
}

type Document struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	DatasetID   string        `json:"dataset_id"`
	Run         ParsingStatus `json:"run"`
	Progress    float64       `json:"progress"`
	ProgressMsg string        `json:"progress_msg"`
	Status      string        `json:"status"`
	ChunkCount  int           `json:"chunk_count"`
	TokenCount  int           `json:"token_count"`
}

type DocumentList struct {
	Total     int        `json:"total"`
	Documents []Document `json:"docs"`
}

type Chunk struct {
	ID                string   `json:"id"`
	Content           string   `json:"content"`
	DatasetID         string   `json:"dataset_id"`
	DocumentID        string   `json:"document_id"`
	DocumentName      string   `json:"document_name"`
	DocumentKeyword   string   `json:"document_keyword"`
	ImportantKeywords []string `json:"important_keywords"`
	Questions         []string `json:"questions"`
	TagKeywords       []string `json:"tag_kwd"`
	Similarity        float64  `json:"similarity"`
	VectorSimilarity  float64  `json:"vector_similarity"`
	TermSimilarity    float64  `json:"term_similarity"`
	Available         bool     `json:"available"`
}

type ChunkList struct {
	Total  int     `json:"total"`
	Chunks []Chunk `json:"chunks"`
}

type DocumentAggregation struct {
	Count        int    `json:"count"`
	DocumentID   string `json:"doc_id"`
	DocumentName string `json:"doc_name"`
}

type RetrievalResult struct {
	Total        int                   `json:"total"`
	Chunks       []Chunk               `json:"chunks"`
	DocumentAggs []DocumentAggregation `json:"doc_aggs"`
}

type Chat struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	DatasetIDs []string `json:"dataset_ids"`
	LLMID      string   `json:"llm_id"`
}

type ChatList struct {
	Total int    `json:"total"`
	Chats []Chat `json:"chats"`
}

type DatasetListOptions struct {
	Page                 int
	PageSize             int
	OrderBy              string
	Desc                 *bool
	ID                   string
	Name                 string
	IncludeParsingStatus *bool
}

type DocumentListOptions struct {
	Page           int
	PageSize       int
	OrderBy        string
	Desc           *bool
	ID             string
	IDs            []string
	Name           string
	Keywords       string
	CreateTimeFrom int64
	CreateTimeTo   int64
	Suffix         string
	Run            string
}

type ChunkListOptions struct {
	Page     int
	PageSize int
	ID       string
	Keywords string
}

type ChatListOptions struct {
	Page     int
	PageSize int
	OrderBy  string
	Desc     *bool
	ID       string
	Name     string
	Keywords string
}

// WaitForParsedOptions controls asynchronous document-index polling.
// Zero values select a one-second poll interval and a fifteen-minute timeout.
type WaitForParsedOptions struct {
	PollInterval time.Duration
	Timeout      time.Duration
	OnProgress   func(Document)
}

// NormalizeParsingStatus maps RAGFlow's textual and numeric state forms to a
// stable public value. Unknown values are returned upper-cased so callers can
// inspect newer server states without losing information.
func NormalizeParsingStatus(value string) ParsingStatus {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "3", "DONE":
		return ParsingStatusDone
	case "2", "CANCEL", "CANCELLED":
		return ParsingStatusCanceled
	case "4", "FAIL", "FAILED":
		return ParsingStatusFailed
	case "1", "RUNNING":
		return ParsingStatusRunning
	case "0", "UNSTART":
		return ParsingStatusUnstarted
	case "5", "SCHEDULE", "SCHEDULED":
		return ParsingStatusScheduled
	default:
		return ParsingStatus(strings.ToUpper(strings.TrimSpace(value)))
	}
}

// UnmarshalJSON accepts RAGFlow's normal textual states and compatible numeric
// state values, then exposes the canonical textual status to callers.
func (s *ParsingStatus) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*s = NormalizeParsingStatus(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	*s = NormalizeParsingStatus(number.String())
	return nil
}

func Bool(value bool) *bool {
	return &value
}
