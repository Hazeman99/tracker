//go:build dev

package main

import "net/http"

const devBuild = true

// In dev builds the frontend is served by the Vite dev server (hot reload
// needs a websocket and inline styles), so the CSP is not applied.
func securityHeaders(next http.Handler) http.Handler { return next }
