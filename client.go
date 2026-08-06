package searagsdk

// Client provides RAGFlow API resources through the SeaArt gateway.
type Client struct {
	Endpoint  string
	APIKey    string
	Transport *Transport

	Datasets  *DatasetsResource
	Documents *DocumentsResource
	Chunks    *ChunksResource
	Retrieval *RetrievalResource
	Chat      *ChatResource
	Raw       *RawResource
}

func NewClient(options ClientOptions) *Client {
	endpoint := NormalizeRAGEndpoint(options.Endpoint)
	transport := NewTransport(endpoint, options.APIKey, options.Headers, options.HTTPClient)

	client := &Client{
		Endpoint:  endpoint,
		APIKey:    options.APIKey,
		Transport: transport,
	}
	client.Datasets = &DatasetsResource{transport: transport}
	client.Documents = &DocumentsResource{transport: transport}
	client.Chunks = &ChunksResource{transport: transport}
	client.Retrieval = &RetrievalResource{transport: transport}
	client.Chat = &ChatResource{transport: transport}
	client.Raw = &RawResource{transport: transport}
	return client
}
