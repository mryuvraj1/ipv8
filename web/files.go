package web

import "embed"

// Files contains the panel assets served by the Go binary.
//
//go:embed index.html app.js app.css
var Files embed.FS
