package server

import (
	"fmt"
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
	// HEAD is HTTP HEAD request type
	HEAD string = "HEAD"
)

type endpointHandler func(*webContext)

// EndpointArg represents an API endpoint argument.
type EndpointArg struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

// Endpoint represents an API endpoint definition.
type Endpoint struct {
	Name         string
	Path         string
	Method       string
	CSRFRequired bool
	NoAuth       bool
	AdminOnly    bool
	Handler      endpointHandler `json:"-"`
	Description  string
	Args         []*EndpointArg
}

func (e *Endpoint) Pattern() string {
	return fmt.Sprintf("%s %s", e.Method, e.Path)
}

// Endpoints contains all registered API endpoints.
var Endpoints []*Endpoint

func init() {
	// TODO add Args
	Endpoints = []*Endpoint{
		{
			Name:         "Config",
			Path:         "/api/config",
			Method:       GET,
			CSRFRequired: true,
			NoAuth:       true,
			Handler:      serveConfig,
			Description:  "Serve config",
		},
		{
			Name:         "Search",
			Path:         "/search",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveSearch,
			Description:  "Search websocket endpoint",
		},
		// tmp added for backward compatibility
		{
			Name:         "Add",
			Path:         "/api/add",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveAdd,
			Description:  "Add document form",
		},
		{
			Name:         "Add post",
			Path:         "/api/add",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveAdd,
			Description:  "Save added document",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to add",
				},
				{
					Name:        "title",
					Type:        "string",
					Required:    false,
					Description: "Optional title of the document",
				},
			},
		},
		// alias for /api/add - backward compatibility - use /api/add in the future
		{
			Name:         "Add post",
			Path:         "/add",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveAdd,
			Description:  "Save added document",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document to add",
				},
				{
					Name:        "title",
					Type:        "string",
					Required:    false,
					Description: "Optional title of the document",
				},
			},
		},
		{
			Name:         "Get document",
			Path:         "/api/document",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveGet,
			Description:  "Get document by URL",
			Args: []*EndpointArg{
				{
					Name:        "url",
					Type:        "string",
					Required:    true,
					Description: "URL of the document",
				},
			},
		},
		{
			Name:         "Rules",
			Path:         "/api/rules",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveRules,
			Description:  "Rules page",
		},
		{
			Name:         "Save rules",
			Path:         "/api/rules",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveRules,
			Description:  "Save rules",
		},
		{
			Name:         "History",
			Path:         "/api/history",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveHistory,
			Description:  "Display latest indexed websites",
		},
		{
			Name:         "Add history item",
			Path:         "/api/history",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveSaveHistory,
			Description:  "Add new history item",
		},
		{
			Name:         "Delete",
			Path:         "/api/delete",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveDelete,
			Description:  "Delete documents matching a search query. Non-admin users are restricted to their own documents.",
			Args: []*EndpointArg{
				{Name: "query", Type: "string", Required: true, Description: "Search query string selecting documents to delete (same syntax as the search endpoint)"},
			},
		},
		{
			Name:         "Delete alias",
			Path:         "/api/delete_alias",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveDeleteAlias,
			Description:  "Delete alias",
		},
		{
			Name:         "Add alias",
			Path:         "/api/add_alias",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveAddAlias,
			Description:  "Add alias",
		},
		{
			Name:         "Readable",
			Path:         "/api/readable",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveReadable,
			Description:  "Readabilty view",
		},
		{
			Name:         "Stats",
			Path:         "/api/stats",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveStats,
			Description:  "Search engine statistics",
		},
		{
			Name:         "File",
			Path:         "/api/file",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveFile,
			Description:  "Serve local file content",
			Args: []*EndpointArg{
				{
					Name:        "path",
					Type:        "string",
					Required:    true,
					Description: "Absolute path to the file",
				},
			},
		},
		{
			Name:         "Batch",
			Path:         "/api/batch",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveBatch,
			Description:  "Execute multiple operations (add/delete/get) in a single request",
			Args: []*EndpointArg{
				{Name: "ops", Description: "Array of operations", Required: true},
			},
		},
		{
			Name:         "Reindex",
			Path:         "/api/reindex",
			Method:       POST,
			CSRFRequired: true,
			AdminOnly:    true,
			Handler:      serveReindex,
			Description:  "Reindex all documents",
			Args: []*EndpointArg{
				{Name: "skipSensitive", Type: "bool", Required: false, Description: "Skip documents matching sensitive content patterns"},
				{Name: "detectLanguages", Type: "bool", Required: false, Description: "Enable language detection during reindex"},
			},
		},
		{
			Name:         "API",
			Path:         "/api",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveAPI,
			Description:  "API documentation",
		},
		{
			Name:         "Login",
			Path:         "/api/login",
			Method:       POST,
			CSRFRequired: true,
			NoAuth:       true,
			Handler:      serveLogin,
			Description:  "Login with username and password",
			Args: []*EndpointArg{
				{
					Name:        "username",
					Type:        "string",
					Required:    true,
					Description: "Username of the user",
				},
				{
					Name:        "password",
					Type:        "string",
					Required:    true,
					Description: "Password of the user",
				},
			},
		},
		{
			Name:         "Logout",
			Path:         "/api/logout",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveLogout,
			Description:  "Logout current user",
		},
		{
			Name:         "Profile",
			Path:         "/api/profile",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveProfile,
			Description:  "Get current user profile",
		},
		{
			Name:         "GenerateToken",
			Path:         "/api/profile/token",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveGenerateToken,
			Description:  "Generate a new access token for the current user",
			Args: []*EndpointArg{
				{
					Name:        "description",
					Type:        "string",
					Required:    false,
					Description: "Optional description for the generated token",
				},
			},
		},
	}
}
