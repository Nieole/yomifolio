package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A response that fits the transport envelope passes through unchanged, and one that does not is
// replaced by a bounded refusal rather than streamed.
func TestServeBoundedMCPCapsTheTransportEnvelope(t *testing.T) {
	payload := strings.Repeat("x", 300<<10)
	for _, tc := range []struct {
		name   string
		body   string
		status int
		want   int
	}{
		{name: "fits", body: payload, status: http.StatusOK, want: http.StatusOK},
		{name: "exceeds", body: strings.Repeat("x", mcpWireBudget+1), status: http.StatusOK, want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			recorder := httptest.NewRecorder()
			serveBoundedMCP(recorder, httptest.NewRequest("POST", "/mcp", nil), handler)
			if recorder.Code != tc.want {
				t.Fatalf("status %d want %d", recorder.Code, tc.want)
			}
			if recorder.Body.Len() > mcpWireBudget {
				t.Fatalf("response %d bytes exceeds the transport envelope", recorder.Body.Len())
			}
			if tc.want == http.StatusOK && recorder.Body.String() != tc.body {
				t.Fatal("an admitted response was altered")
			}
		})
	}
}

// The envelope covers a doubled application result, so a discovery page near its own byte limit
// still reaches the client in both content and structuredContent.
func TestMCPWireBudgetCoversBothRepresentations(t *testing.T) {
	largestResult := 2 * (262144 + 27)
	if mcpWireBudget < largestResult {
		t.Fatalf("envelope %d cannot carry a duplicated %d-byte result", mcpWireBudget, largestResult)
	}
}

func TestGrantedToolsFollowsGrantedCapabilities(t *testing.T) {
	tools := newGrantedTools([]string{"read", "cleanup"})
	for _, name := range []string{"library_find_series", "library_get_page_thumbnails", "library_cleanup_metadata_operation", "library_get_run"} {
		if !tools.allows(name) {
			t.Fatalf("granted tool %s hidden", name)
		}
	}
	for _, name := range []string{"library_enrich_metadata", "library_start_scan", "library_assess_match"} {
		if tools.allows(name) {
			t.Fatalf("ungranted tool %s offered", name)
		}
	}
	if newGrantedTools(nil).allows("library_get_run") {
		t.Fatal("a credential without any grant still sees its run reads")
	}
	if unknown := newGrantedTools(nil); !unknown.allows("library_future_tool") {
		t.Fatal("a tool outside the table stays visible until the table names it")
	}
}
