package server

import (
	"fmt"

	"github.com/asciimoo/hister/config"
)

const (
	// GET is HTTP GET request type
	GET string = "GET"
	// POST is HTTP POST request type
	POST string = "POST"
	// PUT is HTTP PUT request type
	PUT string = "PUT"
	// PATCH is HTTP PATCH request type
	PATCH string = "PATCH"
	// DELETE is HTTP DELETE request type
	DELETE string = "DELETE"
	// HEAD is HTTP HEAD request type
	HEAD string = "HEAD"
)

type endpointHandler func(*webContext)

// EndpointArg represents a query-string or form parameter for an API endpoint.
type EndpointArg struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// JSONSchemaField represents a single field in a JSON request body schema.
// Fields may be nested to describe object and array element types.
type JSONSchemaField struct {
	Name        string             `json:"name"`
	Type        string             `json:"type"`
	Required    bool               `json:"required"`
	Description string             `json:"description"`
	Fields      []*JSONSchemaField `json:"fields,omitempty"`
}

// Endpoint represents an API endpoint definition.
type Endpoint struct {
	Name         string
	Path         string
	Method       string
	CSRFRequired bool
	NoAuth       bool
	AdminOnly    bool
	Public       bool
	NoStore      bool
	Handler      endpointHandler `json:"-"`
	// BodyLimit overrides the default request body limit, using the active config.
	BodyLimit   func(*config.Config) int64 `json:"-"`
	Description string
	Args        []*EndpointArg
	// JSONSchema describes the fields of the JSON request body, if the endpoint
	// consumes application/json instead of (or in addition to) form data.
	JSONSchema []*JSONSchemaField
}

func (e *Endpoint) Pattern() string {
	return fmt.Sprintf("%s %s", e.Method, e.Path)
}

// Endpoints contains all registered API endpoints.
var Endpoints []*Endpoint

const skipRulesOverrideDescription = " Set metadata.ignore_skip_rules to boolean true in the submitted JSON document to bypass URL allow and skip rules. The saved metadata also bypasses these rules on import and reindex. Other validation still applies."

func documentLanguageOverrideSchema() *JSONSchemaField {
	return &JSONSchemaField{
		Name:        "language_override",
		Type:        "string",
		Description: "Optional persistent language tag, such as en-US, nb, uk, or unknown. Tags are normalized; languages without an analyzer use the default analyzer. Preserved in exports and imports. To change or clear an existing pin, use /api/update; language auto resumes detection.",
	}
}

func documentMetadataSchema() *JSONSchemaField {
	return &JSONSchemaField{
		Name:        "metadata",
		Type:        "object",
		Description: "Additional document metadata",
		Fields: []*JSONSchemaField{
			{
				Name:        "ignore_skip_rules",
				Type:        "bool",
				Description: "Explicitly bypass URL allow and skip rules for this document, including during index rebuilds. Only boolean true enables the override.",
			},
		},
	}
}

func init() {
	Endpoints = []*Endpoint{
		{
			Name:        "Metrics",
			Path:        "/metrics",
			Method:      GET,
			AdminOnly:   true,
			Handler:     serveMetrics,
			Description: "Prometheus metrics endpoint for server monitoring. Returns metrics in Prometheus exposition format. Enabled via the server.metrics configuration toggle.",
		},
		{
			Name:        "Diagnostics",
			Path:        "/api/diagnostics",
			Method:      GET,
			AdminOnly:   true,
			Handler:     serveDiagnostics,
			Description: "Inspect index compatibility and enabled extractor dependencies without modifying server data. Requires admin access in multi user mode. Returns checks with name, status (ok or error), and message fields.",
		},
		{
			Name:         "Config",
			Path:         "/api/config",
			Method:       GET,
			CSRFRequired: true,
			NoAuth:       true,
			Public:       true,
			Handler:      serveConfig,
			Description:  "Return public configuration, search capabilities, authentication mode, CSRF state, and canConfigure for settings navigation. Includes the running Hister version in the version field only when server.expose_version is enabled.",
		},
		{
			Name:        "Settings",
			Path:        "/api/settings",
			Method:      GET,
			AdminOnly:   true,
			NoStore:     true,
			Handler:     serveSettings,
			Description: "Read configuration field descriptions, redacted active values, sources, editability, and an opaque revision. Requires administrative access. Does not save or apply settings.",
		},
		{
			Name:         "ValidateSettings",
			Path:         "/api/settings/validate",
			Method:       POST,
			AdminOnly:    true,
			NoStore:      true,
			CSRFRequired: true,
			Handler:      serveValidateSettings,
			Description:  "Validate a complete configuration candidate without saving or applying it. Accepts ordinary App edits only. Returns redacted candidate values on success, field errors with status 422, or status 409 when the revision or configuration sources have changed.",
			JSONSchema: []*JSONSchemaField{
				{Name: "revision", Type: "string", Required: true, Description: "Revision returned by GET /api/settings."},
				{Name: "edits", Type: "array", Required: true, Description: "Explicit setting operations; an empty array validates the inherited configuration.", Fields: []*JSONSchemaField{
					{Name: "path", Type: "string", Required: true, Description: "Stable setting path from the field catalog."},
					{Name: "action", Type: "string", Required: true, Description: "set or reset. Each path may appear only once."},
					{Name: "value", Type: "any", Description: "Typed value required for set and forbidden for reset."},
				}},
			},
		},
		{
			Name:         "ApplySettings",
			Path:         "/api/settings/apply",
			Method:       POST,
			AdminOnly:    true,
			NoStore:      true,
			CSRFRequired: true,
			Handler:      serveApplySettings,
			Description:  "Save and apply ordinary App settings. Uses the revision and explicit edits format of POST /api/settings/validate. Changed selections briefly interrupt service; unchanged selections preserve the active revision without interruption. Returns 409 for changed sources or an outdated revision and 422 for invalid settings. Read GET /api/settings after success or a lost response to obtain the active revision. Sensitive access settings remain read only.",
			JSONSchema: []*JSONSchemaField{
				{Name: "revision", Type: "string", Required: true, Description: "Revision returned by GET /api/settings."},
				{Name: "edits", Type: "array", Required: true, Description: "Explicit setting operations using path, action (set or reset), and value for set."},
			},
		},
		{
			Name:        "Search",
			Path:        "/search",
			Method:      GET,
			Public:      true,
			Handler:     serveSearch,
			Description: "Search endpoint. With a query parameter it returns JSON results directly. Without one it upgrades to a WebSocket connection that accepts repeated JSON Query messages and streams back results.",
			Args: []*EndpointArg{
				{
					Name:        "q",
					Type:        "string",
					Required:    false,
					Description: "Search query, including filters and directives such as sort:date or sort:-date",
				},
				{
					Name:        "query",
					Type:        "string (JSON)",
					Required:    false,
					Description: "JSON-encoded Query object; used as an alternative to individual parameters",
				},
				{
					Name:        "date_from",
					Type:        "string (YYYY-MM-DD)",
					Required:    false,
					Description: "Return only results updated on or after this date",
				},
				{
					Name:        "date_to",
					Type:        "string (YYYY-MM-DD)",
					Required:    false,
					Description: "Return only results updated on or before this date",
				},
				{
					Name:        "include_html",
					Type:        "string (0 or 1)",
					Required:    false,
					Description: "Include raw HTML in results",
				},
				{
					Name:        "page_key",
					Type:        "string",
					Required:    false,
					Description: "Pagination cursor returned by a previous response",
				},
				{
					Name:        "sort",
					Type:        "string",
					Required:    false,
					Description: "Legacy sort order. Prefer a sort directive in q. Prefix a value with a minus sign to reverse it",
				},
				{
					Name:        "semantic",
					Type:        "string (0/1/true/false)",
					Required:    false,
					Description: "Enable semantic search",
				},
				{
					Name:        "semantic_threshold",
					Type:        "float",
					Required:    false,
					Description: "Minimum similarity score for semantic results",
				},
			},
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "text",
					Type:        "string",
					Required:    false,
					Description: "Search query string, including filters and sort directives",
				},
				{
					Name:        "date_from",
					Type:        "int64",
					Required:    false,
					Description: "Unix timestamp lower bound for updated date",
				},
				{
					Name:        "date_to",
					Type:        "int64",
					Required:    false,
					Description: "Unix timestamp upper bound for updated date",
				},
				{
					Name:        "include_html",
					Type:        "bool",
					Required:    false,
					Description: "Include raw HTML in results",
				},
				{
					Name:        "page_key",
					Type:        "string",
					Required:    false,
					Description: "Pagination cursor from a previous response",
				},
				{
					Name:        "sort",
					Type:        "string",
					Required:    false,
					Description: "Legacy sort order. Prefer a sort directive in text. Prefix a value with a minus sign to reverse it",
				},
				{
					Name:        "limit",
					Type:        "int",
					Required:    false,
					Description: "Maximum number of results",
				},
				{
					Name:        "highlight",
					Type:        "string",
					Required:    false,
					Description: "Field to highlight matched terms in",
				},
				{
					Name:        "semantic_enabled",
					Type:        "bool",
					Required:    false,
					Description: "Enable semantic search",
				},
				{
					Name:        "semantic_threshold",
					Type:        "float64",
					Required:    false,
					Description: "Minimum similarity score for semantic results",
				},
				{
					Name:        "semantic_weight",
					Type:        "float64",
					Required:    false,
					Description: "Weight applied to semantic vs full-text scores",
				},
				{
					Name:        "facets",
					Type:        "bool",
					Required:    false,
					Description: "Include facet counts (domain, language) in the response",
				},
				{
					Name:        "facet_term_size",
					Type:        "int",
					Required:    false,
					Description: "Override the default top-N cap for term facets",
				},
			},
		},
		{
			Name:        "Suggest",
			Path:        "/suggest",
			Method:      GET,
			Public:      true,
			Handler:     serveSuggest,
			Description: "OpenSearch suggestions endpoint; returns pinned and history results before index results, with descriptions and target URLs",
			Args: []*EndpointArg{
				{
					Name:        "q",
					Type:        "string",
					Required:    true,
					Description: "Partial query string to complete",
				},
			},
		},
		{
			Name:         "Add",
			Path:         "/api/add",
			Method:       POST,
			BodyLimit:    documentBodyLimit,
			CSRFRequired: true,
			Handler:      serveAdd,
			Description:  "Index a document. Accepts either application/x-www-form-urlencoded or application/json." + skipRulesOverrideDescription,
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to index",
				},
				{
					Name:        "title",
					Type:        "string",
					Required:    false,
					Description: "Document title",
				},
				{
					Name:        "text",
					Type:        "string",
					Required:    false,
					Description: "Plain-text content",
				},
			},
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to index",
				},
				{
					Name:        "title",
					Type:        "string",
					Required:    false,
					Description: "Document title",
				},
				{
					Name:        "text",
					Type:        "string",
					Required:    false,
					Description: "Plain-text content (overrides server-side HTML extraction)",
				},
				{
					Name:        "html",
					Type:        "string",
					Required:    false,
					Description: "Raw HTML source (text is extracted server-side)",
				},
				{
					Name:        "favicon",
					Type:        "string",
					Required:    false,
					Description: "Base64-encoded favicon data URI",
				},
				{
					Name:        "label",
					Type:        "string",
					Required:    false,
					Description: "User-defined label for the document",
				},
				{
					Name:        "type",
					Type:        "int",
					Required:    false,
					Description: "Document type. Use 2 with a remote-file URL for locally extracted file snapshots.",
				},
				{
					Name:        "updated",
					Type:        "int64",
					Required:    false,
					Description: "Source modification time as a Unix timestamp for remote file snapshots",
				},
				documentLanguageOverrideSchema(),
				documentMetadataSchema(),
			},
		},
		{
			Name:         "Add PDF",
			Path:         "/api/add_pdf",
			Method:       POST,
			BodyLimit:    documentBodyLimit,
			CSRFRequired: true,
			Handler:      serveAddPDF,
			Description:  "Index a PDF document. Accepts application/json with a document object and base64-encoded PDF content." + skipRulesOverrideDescription,
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "document",
					Type:        "object",
					Required:    true,
					Description: "Document metadata",
					Fields: []*JSONSchemaField{
						{
							Name:        "url",
							Type:        "string",
							Required:    true,
							Description: "URL or file:// URI identifying the PDF",
						},
						{
							Name:        "title",
							Type:        "string",
							Required:    false,
							Description: "Document title",
						},
						{
							Name:        "label",
							Type:        "string",
							Required:    false,
							Description: "User-defined label",
						},
						documentLanguageOverrideSchema(),
						documentMetadataSchema(),
					},
				},
				{
					Name:        "pdf",
					Type:        "string",
					Required:    true,
					Description: "Base64-encoded PDF binary content",
				},
			},
		},
		{
			Name:         "Update label",
			Path:         "/api/label",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveUpdateLabel,
			Description:  "Update (or clear) the user-defined label of a stored document",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to label",
				},
				{
					Name:        "label",
					Type:        "string",
					Required:    false,
					Description: "New label value; pass empty string to clear",
				},
			},
		},
		{
			Name:         "Document versions",
			Path:         "/api/versions",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveVersions,
			Description:  "Return all stored version diffs for a document URL. Versions are recorded when the URL matches a versioning rule and the document is re-indexed.",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to retrieve versions for",
				},
				{
					Name:        "document_id",
					Type:        "string",
					Required:    false,
					Description: "Exact document ID when the URL exists under more than one owner",
				},
			},
		},
		{
			Name:         "Get document",
			Path:         "/api/document",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveGet,
			Description:  "Retrieve a stored document by its URL",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document",
				},
				{
					Name:        "document_id",
					Type:        "string",
					Required:    false,
					Description: "Exact document ID when the URL exists under more than one owner",
				},
			},
		},
		{
			Name:         "Get facets",
			Path:         "/api/facets",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveGetFacets,
			Description:  "Return all configured facet counts for a query without fetching documents",
			Args: []*EndpointArg{
				{
					Name:        "q",
					Type:        "string",
					Required:    true,
					Description: "Search query text",
				},
				{
					Name:        "date_from",
					Type:        "integer",
					Required:    false,
					Description: "Unix timestamp lower bound for the updated field",
				},
				{
					Name:        "date_to",
					Type:        "integer",
					Required:    false,
					Description: "Unix timestamp upper bound for the updated field",
				},
				{
					Name:        "size_{name}",
					Type:        "integer",
					Required:    false,
					Description: "Override the configured term limit for a named facet",
				},
			},
		},
		{
			Name:         "Update documents",
			Path:         "/api/update",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveUpdateDocuments,
			Description:  "Update mutable attributes on documents matching a search query. Regular users are restricted to their own documents. Changing ownership requires an administrator.",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "query",
					Type:        "string",
					Required:    true,
					Description: "Search query selecting documents to update",
				},
				{
					Name:        "changes",
					Type:        "object",
					Required:    true,
					Description: "Document attributes to change",
					Fields: []*JSONSchemaField{
						{Name: "user_id", Type: "integer", Required: false, Description: "New owner user ID; 0 makes the document global"},
						{Name: "label", Type: "string", Required: false, Description: "New label; an empty value clears it"},
						{Name: "title", Type: "string", Required: false, Description: "New title; an empty value clears it"},
						{Name: "language", Type: "string", Required: false, Description: "Pin a known language tag, such as en-US or nb, or unknown across reindexing and resubmission. Tags are normalized independently of analyzer availability. Use auto to clear the pin and detect the stored text using current settings."},
					},
				},
				{
					Name:        "dry_run",
					Type:        "bool",
					Required:    false,
					Description: "Return counts without changing documents",
				},
			},
		},
		{
			Name:         "Rules",
			Path:         "/api/rules",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveRules,
			Description:  "Retrieve current allow, skip, priority, and versioning rules and query aliases",
		},
		{
			Name:         "Save rules",
			Path:         "/api/rules",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveRules,
			Description:  "Update the supplied allow, skip, priority, or versioning rules. Omitted rule groups remain unchanged. Accepts application/x-www-form-urlencoded.",
			Args: []*EndpointArg{
				{
					Name:        "allow",
					Type:        "string",
					Required:    false,
					Description: "Space-separated URL regex patterns. When nonempty, a URL must match at least one to be indexed or retained during reindex. Skip rules take precedence. Explicit manual overrides bypass both rule groups.",
				},
				{
					Name:        "skip",
					Type:        "string",
					Required:    false,
					Description: "Space-separated list of URL regex patterns to skip during indexing",
				},
				{
					Name:        "priority",
					Type:        "string",
					Required:    false,
					Description: "Space-separated list of URL regex patterns to surface first in results",
				},
				{
					Name:        "versioning",
					Type:        "string",
					Required:    false,
					Description: "Space-separated list of URL regex patterns for which document versions are tracked",
				},
			},
		},
		{
			Name:         "Preview rule",
			Path:         "/api/rules/preview",
			Method:       POST,
			BodyLimit:    func(*config.Config) int64 { return maxRulePreviewBytes },
			CSRFRequired: true,
			Handler:      serveRulePreview,
			Description:  "Validate a draft rule or generate a domain or exact URL rule, and optionally test a URL using the indexing rule matcher. Returns pattern and, when a test URL is supplied, matches. Does not fetch URLs or save rules. Tests only the supplied draft, without evaluating other saved rules. Provide exactly one of pattern, domain, or exact_url.",
			JSONSchema: []*JSONSchemaField{
				{Name: "pattern", Type: "string", Description: "A single Go regexp pattern."},
				{Name: "domain", Type: "string", Description: "Domain or HTTP page URL from which to generate a hostname rule. Applies to HTTP and HTTPS on any port."},
				{Name: "exact_url", Type: "string", Description: "Complete URL to match literally, including scheme, path, and query. Use the URL as stored in the index. No URL normalization is applied."},
				{Name: "include_subdomains", Type: "bool", Description: "Include subdomains in the generated domain rule. Defaults to false."},
				{Name: "url", Type: "string", Description: "Optional complete URL to test against the draft rule."},
			},
		},
		{
			Name:         "History",
			Path:         "/api/history",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveHistory,
			Description:  "Retrieve recently indexed documents or search query history",
			Args: []*EndpointArg{
				{
					Name:        "opened",
					Type:        "bool",
					Required:    false,
					Description: "When \"true\", returns search query history instead of recently indexed documents",
				},
				{
					Name:        "last_id",
					Type:        "uint",
					Required:    false,
					Description: "Pagination cursor: last history item ID from a previous response (used with opened=true)",
				},
				{
					Name:        "last_updated_at",
					Type:        "string",
					Required:    false,
					Description: "Pagination cursor timestamp from a previous response, used with last_id and opened=true",
				},
				{
					Name:        "last",
					Type:        "string",
					Required:    false,
					Description: "Pagination cursor: URL of the last indexed document from a previous response",
				},
				{
					Name:        "filter",
					Type:        "string",
					Required:    false,
					Description: "Case insensitive title or URL filter applied before pagination",
				},
				{
					Name:        "date_from",
					Type:        "integer",
					Required:    false,
					Description: "Inclusive Unix timestamp lower bound",
				},
				{
					Name:        "date_to",
					Type:        "integer",
					Required:    false,
					Description: "Exclusive Unix timestamp upper bound",
				},
				{
					Name:        "format",
					Type:        "string",
					Required:    false,
					Description: "Response format; set to \"rss\" to receive an RSS 2.0 feed instead of JSON",
				},
			},
		},
		{
			Name:         "History timeline",
			Path:         "/api/history/timeline",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveHistoryTimeline,
			Description:  "Return hierarchical date counts for the history view",
			Args: []*EndpointArg{
				{
					Name:        "opened",
					Type:        "bool",
					Required:    false,
					Description: "When true, count opened result history instead of indexed documents",
				},
				{
					Name:        "filter",
					Type:        "string",
					Required:    false,
					Description: "Case insensitive title or URL filter",
				},
				{
					Name:        "timezone",
					Type:        "string",
					Required:    false,
					Description: "IANA timezone used to construct calendar boundaries",
				},
				{
					Name:        "date_from",
					Type:        "integer",
					Required:    false,
					Description: "Inclusive Unix timestamp lower bound for daily drilldown; requires date_to",
				},
				{
					Name:        "date_to",
					Type:        "integer",
					Required:    false,
					Description: "Exclusive Unix timestamp upper bound for daily drilldown; requires date_from",
				},
			},
		},
		{
			Name:         "Add history item",
			Path:         "/api/history",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveSaveHistory,
			Description:  "Record or delete a search query history entry",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "url",
					Type:        "string",
					Required:    false,
					Description: "URL of the visited page",
				},
				{
					Name:        "title",
					Type:        "string",
					Required:    false,
					Description: "Page title",
				},
				{
					Name:        "query",
					Type:        "string",
					Required:    false,
					Description: "Search query string that led to this page",
				},
				{
					Name:        "delete",
					Type:        "bool",
					Required:    false,
					Description: "When true, removes the matching history entry instead of adding one",
				},
				{
					Name:        "pin",
					Type:        "bool",
					Required:    false,
					Description: "When set, pins (true) or unpins (false) the entry as a priority result; unpinning keeps it in the visited history",
				},
			},
		},
		{
			Name:         "Delete",
			Path:         "/api/delete",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveDelete,
			Description:  "Delete documents matching a search query. Non-admin users are restricted to their own documents.",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "query",
					Type:        "string",
					Required:    true,
					Description: "Search query string selecting documents to delete (same syntax as the search endpoint)",
				},
				{
					Name:        "dry_run",
					Type:        "bool",
					Required:    false,
					Description: "Return the number of matching documents without deleting them",
				},
			},
		},
		{
			Name:        "List users",
			Path:        "/api/users",
			Method:      GET,
			AdminOnly:   true,
			Handler:     serveUsers,
			Description: "List account metadata in ascending ID order. Requires administrator access and user handling. Returns users and, when another page exists, next_after_id. Credentials and rules are never included.",
			Args: []*EndpointArg{
				{Name: "username", Type: "string", Description: "Optional exact, case sensitive username filter"},
				{Name: "limit", Type: "int", Description: "Page size, from 1 to 100 (default 50)"},
				{Name: "after_id", Type: "int", Description: "Return IDs greater than this value (default 0). Use next_after_id from the previous page."},
			},
		},
		{
			Name:         "Create user",
			Path:         "/api/users",
			Method:       POST,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveCreateUser,
			Description:  "Create an account on this server. Requires user handling. Returns 201 with account metadata and a Location header, or 409 if the username already exists. Credentials are never included in the response.",
			JSONSchema: []*JSONSchemaField{
				{Name: "username", Type: "string", Required: true, Description: "Case sensitive username. Must not be blank or contain control characters."},
				{Name: "password", Type: "string", Required: true, Description: "Password containing 8 to 72 UTF-8 bytes."},
				{Name: "is_admin", Type: "bool", Description: "Grant administrator privileges (default false)."},
			},
		},
		{
			Name:        "Get user",
			Path:        "/api/users/{id}",
			Method:      GET,
			AdminOnly:   true,
			Handler:     serveUser,
			Description: "Return account metadata for the positive numeric user ID in the path: id, username, is_admin, created_at, updated_at, and revision. Requires user handling. Credentials and rules are never included.",
		},
		{
			Name:         "Update user",
			Path:         "/api/users/{id}",
			Method:       PATCH,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveUpdateUser,
			Description:  "Apply account changes together. Requires user handling and the current revision. Returns updated metadata, plus the new token only when rotation is requested. Returns 409 for a stale revision, duplicate username, or demotion of the last administrator.",
			JSONSchema: []*JSONSchemaField{
				{Name: "revision", Type: "int", Required: true, Description: "Current account revision from a user read or creation response."},
				{Name: "username", Type: "string", Description: "New case sensitive username. Must not be blank or contain control characters."},
				{Name: "password", Type: "string", Description: "New password containing 8 to 72 UTF-8 bytes. Invalidates existing browser sessions."},
				{Name: "is_admin", Type: "bool", Description: "Set administrator privileges. The last administrator cannot be demoted."},
				{Name: "regenerate_token", Type: "bool", Description: "Rotate the access token and return the new token (default false)."},
			},
		},
		{
			Name:         "Delete user",
			Path:         "/api/users/{id}",
			Method:       DELETE,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveDeleteUser,
			Description:  "Delete an account by ID. Requires user handling. Accounts with indexed documents require purge. The last administrator cannot be deleted. Returns id, username, and deleted_documents.",
			Args: []*EndpointArg{
				{Name: "purge", Type: "bool", Description: "Remove the account's indexed documents before deletion (default false)."},
			},
		},
		{
			Name:        "Get user token",
			Path:        "/api/users/{id}/token",
			Method:      GET,
			AdminOnly:   true,
			Handler:     serveUserToken,
			Description: "Return the existing access token for the positive numeric user ID in the path, without rotating it. Requires administrator access and user handling. Returns a token object with Cache-Control: no-store.",
		},
		{
			Name:         "Rotate user token",
			Path:         "/api/users/{id}/token",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveRotateUserToken,
			Description:  "Replace the access token for the positive numeric user ID in the path. Requires user handling and the current account revision. Users can rotate their own token; administrators can also rotate other accounts' tokens. Returns token and revision with Cache-Control: no-store, or 409 for a stale revision. The old token stops working; browser sessions remain valid.",
			JSONSchema: []*JSONSchemaField{
				{Name: "revision", Type: "int", Required: true, Description: "Current account revision from a user read or creation response."},
			},
		},
		{
			Name:         "Delete alias",
			Path:         "/api/delete_alias",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveDeleteAlias,
			Description:  "Remove a query alias",
			Args: []*EndpointArg{
				{
					Name:        "alias",
					Type:        "string",
					Required:    true,
					Description: "Alias keyword to remove",
				},
			},
		},
		{
			Name:         "Add alias",
			Path:         "/api/add_alias",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveAddAlias,
			Description:  "Add or update a query alias",
			Args: []*EndpointArg{
				{
					Name:        "alias-keyword",
					Type:        "string",
					Required:    true,
					Description: "Shorthand keyword the user types in search",
				},
				{
					Name:        "alias-value",
					Type:        "string",
					Required:    true,
					Description: "Expanded query expression that the keyword maps to",
				},
			},
		},
		{
			Name:         "Preview",
			Path:         "/api/preview",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      servePreview,
			Description:  "Render a readable preview and return the searchable properties of a stored document",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to preview",
				},
				{
					Name:        "document_id",
					Type:        "string",
					Required:    false,
					Description: "Exact document ID when the URL exists under more than one owner",
				},
				{
					Name:        "extractor",
					Type:        "string",
					Required:    false,
					Description: "Name of the extractor to use (case-insensitive); omit to use the default chain",
				},
			},
		},
		{
			Name:         "Extractors",
			Path:         "/api/extractors",
			Method:       GET,
			CSRFRequired: false,
			NoAuth:       true,
			Public:       true,
			Handler:      serveExtractors,
			Description:  "List all registered extractors, or preview extractors matching a specific document URL",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    false,
					Description: "If provided, return only preview extractors that match this indexed document URL",
				},
				{
					Name:        "document_id",
					Type:        "string",
					Required:    false,
					Description: "Exact document ID when the URL exists under more than one owner",
				},
			},
		},
		{
			Name:         "Stats",
			Path:         "/api/stats",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveStats,
			Description:  "Return index statistics (document count, file count, rule count, recent searches)",
		},
		{
			Name:         "Favicon",
			Path:         "/api/favicon",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveStoredFavicon,
			Description:  "Serve a stored document favicon by favicon_key",
			Args: []*EndpointArg{
				{
					Name:        "key",
					Type:        "string",
					Required:    true,
					Description: "Content-addressed favicon_key returned in document results",
				},
			},
		},
		{
			Name:         "File",
			Path:         "/api/file",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveFile,
			Description:  "Download the original content of a locally indexed file",
			Args: []*EndpointArg{
				{
					Name:        "id",
					Type:        "string",
					Required:    true,
					Description: "Indexed document ID in user_id:url form, or the URL for a shared document",
				},
			},
		},
		{
			Name:         "Batch",
			Path:         "/api/batch",
			Method:       POST,
			BodyLimit:    documentBodyLimit,
			CSRFRequired: true,
			Handler:      serveBatch,
			Description:  "Execute up to 100 add/delete/get operations in a single request (body limit configured by server.max_batch_body_size, default 40 MiB). Each add operation has its own metadata." + skipRulesOverrideDescription,
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "ops",
					Type:        "array",
					Required:    true,
					Description: "List of operations to execute (maximum 100)",
					Fields: []*JSONSchemaField{
						{
							Name:        "op",
							Type:        "string",
							Required:    true,
							Description: "Operation type: \"add\", \"delete\", or \"get\"",
						},
						{
							Name:        "url",
							Type:        "string",
							Required:    true,
							Description: "Document URL",
						},
						{
							Name:        "title",
							Type:        "string",
							Required:    false,
							Description: "Document title (add only)",
						},
						{
							Name:        "text",
							Type:        "string",
							Required:    false,
							Description: "Plain-text content (add only)",
						},
						{
							Name:        "html",
							Type:        "string",
							Required:    false,
							Description: "Raw HTML source (add only; text extracted server-side)",
						},
						{
							Name:        "favicon",
							Type:        "string",
							Required:    false,
							Description: "Base64-encoded favicon data URI (add only)",
						},
						documentLanguageOverrideSchema(),
						documentMetadataSchema(),
					},
				},
			},
		},
		{
			Name:         "Reload",
			Path:         "/api/reload",
			Method:       POST,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveReload,
			Description:  "Apply updated settings to the running server, including rules and logging. Briefly interrupts service. Requires administrator access in multi user mode.",
		},
		{
			Name:         "Cleanup",
			Path:         "/api/cleanup",
			Method:       POST,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveCleanup,
			Description:  "Remove local documents that no longer match configured directories, and remove orphaned HTML and favicon data files (admin only)",
		},
		{
			Name:         "Purge retained data",
			Path:         "/api/purge",
			Method:       POST,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      servePurge,
			Description:  "Permanently remove selected retained SQL records before a cutoff, or preview matching counts (admin only). Current documents are preserved.",
			JSONSchema: []*JSONSchemaField{
				{Name: "before", Type: "string", Required: true, Description: "Exclusive cutoff as an RFC3339 timestamp in the past"},
				{Name: "versions", Type: "bool", Description: "Remove stored document versions created before the cutoff"},
				{Name: "history", Type: "bool", Description: "Remove unpinned opened result history last used before the cutoff, and old unreferenced query and URL records"},
				{Name: "crawl_jobs", Type: "bool", Description: "Remove completed crawl jobs last updated before the cutoff and their URL records; preserve running, interrupted, and queued work"},
				{Name: "dry_run", Type: "bool", Description: "Return matching counts without deleting records"},
			},
		},
		{
			Name:         "Reindex",
			Path:         "/api/reindex",
			Method:       POST,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveReindex,
			Description:  "Rebuild the search index from all stored documents (admin only)",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "skipSensitive",
					Type:        "bool",
					Required:    false,
					Description: "Skip documents that match sensitive-content patterns",
				},
				{
					Name:        "detectLanguages",
					Type:        "bool",
					Required:    false,
					Description: "Enable per-language index routing during reindex",
				},
			},
		},
		{
			Name:         "API",
			Path:         "/api",
			Method:       GET,
			CSRFRequired: false,
			Public:       true,
			Handler:      serveAPI,
			Description:  "Return this API documentation as JSON",
		},
		{
			Name:         "Login",
			Path:         "/api/login",
			Method:       POST,
			CSRFRequired: true,
			NoAuth:       true,
			Handler:      serveLogin,
			Description:  "Authenticate with username and password and create a session",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "username",
					Type:        "string",
					Required:    true,
					Description: "Account username",
				},
				{
					Name:        "password",
					Type:        "string",
					Required:    true,
					Description: "Account password",
				},
			},
		},
		{
			Name:         "TokenLogin",
			Path:         "/api/token-login",
			Method:       POST,
			CSRFRequired: true,
			NoAuth:       true,
			Handler:      serveTokenLogin,
			Description:  "Authenticate with an access token and create a session",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "token",
					Type:        "string",
					Required:    true,
					Description: "Configured access token",
				},
			},
		},
		{
			Name:         "Logout",
			Path:         "/api/logout",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveLogout,
			Description:  "Destroy the current session",
		},
		{
			Name:         "Profile",
			Path:         "/api/profile",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveProfile,
			Description:  "Return the authenticated user's profile information, including user_id and the current account revision in user handling mode",
		},
		{
			Name:        "MCP",
			Path:        "/mcp",
			Method:      GET,
			NoAuth:      true,
			Handler:     serveMCPGet,
			Description: "Return status 405 because server initiated event streams are not supported.",
		},
		{
			Name:        "MCP",
			Path:        "/mcp",
			Method:      POST,
			Public:      true,
			Handler:     serveMCP,
			Description: "Model Context Protocol endpoint (JSON-RPC 2.0 / Streamable HTTP). Exposes search, preview, and history tools to AI assistants.",
			JSONSchema: []*JSONSchemaField{
				{
					Name:        "jsonrpc",
					Type:        "string",
					Required:    true,
					Description: "JSON-RPC version; must be \"2.0\"",
				},
				{
					Name:        "id",
					Type:        "string | number | null",
					Required:    false,
					Description: "Request identifier; omit for notifications",
				},
				{
					Name:        "method",
					Type:        "string",
					Required:    true,
					Description: "RPC method name (e.g. \"tools/call\", \"initialize\", \"tools/list\")",
				},
				{
					Name:        "params",
					Type:        "object",
					Required:    false,
					Description: "Method parameters; shape depends on the method",
					Fields: []*JSONSchemaField{
						{
							Name:        "name",
							Type:        "string",
							Required:    false,
							Description: "Tool name required by tools/call: search, get_facets, get_preview, or get_history",
						},
						{
							Name:        "arguments",
							Type:        "object",
							Required:    false,
							Description: "Tool arguments for tools/call",
							Fields: []*JSONSchemaField{
								{
									Name:        "query",
									Type:        "string",
									Required:    false,
									Description: "Search query string required by the search tool",
								},
								{
									Name:        "limit",
									Type:        "int",
									Required:    false,
									Description: "Maximum search or history results",
								},
								{
									Name:        "date_from",
									Type:        "string",
									Required:    false,
									Description: "Search lower date bound in YYYY-MM-DD format",
								},
								{
									Name:        "date_to",
									Type:        "string",
									Required:    false,
									Description: "Search upper date bound in YYYY-MM-DD format",
								},
								{
									Name:        "semantic",
									Type:        "bool",
									Required:    false,
									Description: "Enable semantic search when configured",
								},
								{
									Name:        "fields",
									Type:        "string[]",
									Required:    false,
									Description: "Extra document fields returned by search",
								},
								{
									Name:        "url",
									Type:        "string",
									Required:    false,
									Description: "Exact document URL required by get_preview",
								},
								{
									Name:        "extractor",
									Type:        "string",
									Required:    false,
									Description: "Optional preview extractor name",
								},
								{
									Name:        "mode",
									Type:        "string",
									Required:    false,
									Description: "History mode: indexed or opened",
								},
								{
									Name:        "page_key",
									Type:        "string",
									Required:    false,
									Description: "Pagination cursor for indexed history",
								},
								{
									Name:        "last_id",
									Type:        "int",
									Required:    false,
									Description: "Pagination cursor for opened history",
								},
							},
						},
					},
				},
			},
		},
		{
			Name:        "OAuthRedirect",
			Path:        "/api/oauth",
			Method:      GET,
			NoAuth:      true,
			Handler:     serveOAuthRedirect,
			Description: "Start OAuth authentication flow for a given provider",
			Args: []*EndpointArg{
				{
					Name:        "provider",
					Type:        "string",
					Required:    true,
					Description: "OAuth provider name (github, google, oidc)",
				},
				{
					Name:        "return_to",
					Type:        "string",
					Description: "Local application page to return to after login, including the configured base path; defaults to the home page",
				},
			},
		},
		{
			Name:        "OAuthCallback",
			Path:        "/api/oauth/callback",
			Method:      GET,
			NoAuth:      true,
			Handler:     serveOAuthCallback,
			Description: "OAuth provider callback handler",
			Args: []*EndpointArg{
				{
					Name:        "provider",
					Type:        "string",
					Required:    true,
					Description: "OAuth provider name",
				},
				{
					Name:        "code",
					Type:        "string",
					Required:    true,
					Description: "Authorization code returned by the provider",
				},
				{
					Name:        "state",
					Type:        "string",
					Required:    true,
					Description: "CSRF state token issued during the redirect step",
				},
			},
		},
	}
}
