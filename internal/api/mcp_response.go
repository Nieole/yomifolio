package api

import (
	"bytes"
	"net/http"

	"yomifolio/internal/application/delegated"
)

// A structured result travels twice on the wire: structuredContent, plus the serialized JSON text
// fallback every client can read. The transport envelope therefore covers two full application
// results, the 64 KiB request cap that bounds a caller-supplied JSON-RPC ID, and SDK framing.
const mcpWireBudget = 2*delegated.MaxImageResultBytes + 64<<10 + 8<<10

// The final wire budget also covers SDK framing and arbitrary caller-supplied
// JSON-RPC IDs, which are not part of the application result reserve.
type mcpResponse struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	overflow bool
}

func (w *mcpResponse) Header() http.Header { return w.header }
func (w *mcpResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *mcpResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if !w.overflow {
		if len(p) > mcpWireBudget-w.body.Len() {
			w.overflow = true
			w.body.Reset()
		} else {
			_, _ = w.body.Write(p)
		}
	}
	return len(p), nil
}
func (w *mcpResponse) Flush() {}
func serveBoundedMCP(w http.ResponseWriter, r *http.Request, handler http.Handler) {
	buffered := &mcpResponse{header: make(http.Header)}
	handler.ServeHTTP(buffered, r)
	if buffered.overflow {
		http.Error(w, "MCP response exceeds the 584 KiB transport budget; select fewer pages or shorten request ID", http.StatusRequestEntityTooLarge)
		return
	}
	for key, values := range buffered.header {
		w.Header()[key] = values
	}
	if buffered.status != 0 {
		w.WriteHeader(buffered.status)
	}
	_, _ = w.Write(buffered.body.Bytes())
}
