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
		&Endpoint{
			Name:         "Index",
			Path:         "/",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveIndex,
			Description:  "Index page",
		},
		&Endpoint{
			Name:         "Search",
			Path:         "/search",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveSearch,
			Description:  "Search websocket endpoint",
		},
		&Endpoint{
			Name:         "Add",
			Path:         "/add",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveAdd,
			Description:  "Add document form",
		},
		&Endpoint{
			Name:         "Add post",
			Path:         "/add",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveAdd,
			Description:  "Save added document",
		},
		&Endpoint{
			Name:         "Rules",
			Path:         "/rules",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveRules,
			Description:  "Rules page",
		},
		&Endpoint{
			Name:         "Save rules",
			Path:         "/rules",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveRules,
			Description:  "Save rules",
		},
		&Endpoint{
			Name:         "Help",
			Path:         "/help",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveHelp,
			Description:  "Help page",
		},
		&Endpoint{
			Name:         "History",
			Path:         "/history",
			Method:       GET,
			CSRFRequired: true,
			Handler:      serveHistory,
			Description:  "History page",
		},
		&Endpoint{
			Name:         "Add history item",
			Path:         "/history",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveHistory,
			Description:  "Add new history item",
		},
		&Endpoint{
			Name:         "Delete",
			Path:         "/delete",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveDeleteDocument,
			Description:  "Delete document endpoint",
		},
		&Endpoint{
			Name:         "Delete alias",
			Path:         "/delete_alias",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveDeleteAlias,
			Description:  "Delete alias",
		},
		&Endpoint{
			Name:         "Add alias",
			Path:         "/add_alias",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveAddAlias,
			Description:  "Add alias",
		},
		&Endpoint{
			Name:         "About",
			Path:         "/about",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveAbout,
			Description:  "About page",
		},
		&Endpoint{
			Name:         "Readable",
			Path:         "/readable",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveReadable,
			Description:  "Readabilty view",
		},
		&Endpoint{
			Name:         "OpenSearch",
			Path:         "/opensearch.xml",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveOpensearch,
			Description:  "OpenSearch XML descriptor",
		},
		&Endpoint{
			Name:         "Favicon",
			Path:         "/favicon.ico",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveFavicon,
			Description:  "Favicon",
		},
		&Endpoint{
			Name:         "Static",
			Path:         "/static/",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveStatic,
			Description:  "Static files",
		},
		&Endpoint{
			Name:         "API",
			Path:         "/api",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveAPI,
			Description:  "API documentation",
		},
		&Endpoint{
			Name:         "API History",
			Path:         "/api/history",
			Method:       GET,
			CSRFRequired: false,
			Handler:      serveAPIHistory,
			Description:  "Recent or top-visited history as JSON (?kind=recent|top&limit=N)",
			Args: []*EndpointArg{
				{Name: "kind", Type: "string", Required: false, Description: "recent (default) or top"},
				{Name: "limit", Type: "integer", Required: false, Description: "Number of results (default 10)"},
			},
		},
	}
}
