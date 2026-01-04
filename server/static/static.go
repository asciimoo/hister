package static

import "embed"

//go:embed favicon.ico
//go:embed js/*.js
//go:embed logo.png
//go:embed style.css

var FS embed.FS
