package client

type HistoryItem struct {
	Query     string `json:"query"`
	Title     string `json:"title"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
}

type historyRequest struct {
	URL    string `json:"url"`
	Title  string `json:"title,omitempty"`
	Query  string `json:"query"`
	Delete bool   `json:"delete,omitempty"`
}

type RulesResponse struct {
	Skip       []string          `json:"skip"`
	Priority   []string          `json:"priority"`
	Versioning []string          `json:"versioning"`
	Aliases    map[string]string `json:"aliases"`
}

// PreviewResponse is the readable document representation returned by the
// preview API. Content is usually sanitized HTML, but extractor-specific
// templates may return structured JSON instead.
type PreviewResponse struct {
	Title        string         `json:"title"`
	Content      string         `json:"content"`
	Template     string         `json:"template"`
	Added        int64          `json:"added"`
	VersionCount int            `json:"version_count"`
	Meta         map[string]any `json:"meta"`
}

// ServerConfig contains the search capabilities advertised by /api/config
// that command-line clients need at runtime.
type ServerConfig struct {
	AuthMode             string  `json:"authMode"`
	Authenticated        bool    `json:"authenticated"`
	DiagnosticsAvailable bool    `json:"diagnosticsAvailable"`
	SemanticEnabled      bool    `json:"semanticEnabled"`
	SemanticWeight       float64 `json:"semanticWeight"`
	SimilarityThreshold  float64 `json:"similarityThreshold"`
}
