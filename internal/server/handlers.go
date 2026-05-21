package server

import (
	"fmt"
	"net/http"
)

// healthHandler handles GET /healthz — the only real operation in this task.
// Returns 200 "pong" for authenticated requests.
// AC10, SR-22: health endpoint is only reachable after successful auth.
func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "pong")
}

// dispatchHandler is the catch-all for unimplemented routes.
// Returns 501 Not Implemented as an explicit extension point for
// future command-exec, mcp-server, file-upload tasks.
// AC10, SR-23: no side effects, no command execution.
func dispatchHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusNotImplemented)
	fmt.Fprint(w, "not implemented")
}
