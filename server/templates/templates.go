// Package templates provides embedded HTML templates for the Omnom web application.
//
// This package embeds Go template files into the compiled binary. Templates are
// used to render HTML pages for the web interface, including:
//   - Page layouts and partials
//   - Bookmark lists and detail views
//   - Feed readers
//   - User settings
//   - Authentication pages
//   - Email templates (HTML and text)
//
// Templates use Go's html/template package for automatic HTML escaping and
// security. They support template inheritance through blocks and includes.
//
// The template system integrates with:
//   - Localization for multi-language support
//   - CSRF token injection
//   - Flash message display
//   - User session data
//
// The FS variable contains all template files and can be parsed by the
// template engine at startup.
package templates

import "embed"

//go:embed *.tpl */*.tpl

// FS contains all embedded template files.
var FS embed.FS
