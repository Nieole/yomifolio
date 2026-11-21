package metadata

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBudgetedModelRequestsNeverRedirectOrReissue(t *testing.T) {
	for _, kind := range []string{"responses", "chat_completions", "ollama"} {
		t.Run(kind, func(t *testing.T) {
			var calls, checks atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
			}))
			defer server.Close()
			var provider ConnectionTester
			switch kind {
			case "responses":
				provider = NewOpenAIProvider(server.URL, "test", "", 2)
			case "chat_completions":
				provider = NewOpenAILegacyProvider(server.URL, "test", "", 2)
			default:
				provider = NewOllamaProvider(server.URL, "test", 2)
			}
			ctx := WithSingleModelRequest(context.Background(), func() error { checks.Add(1); return nil })
			if _, err := provider.TestLLM(ctx, "label only"); err == nil {
				t.Fatal("redirect was accepted")
			}
			if _, err := provider.TestLLM(ctx, "retry"); err == nil {
				t.Fatal("reissue was accepted")
			}
			if calls.Load() != 1 || checks.Load() != 1 {
				t.Fatal(calls.Load(), checks.Load())
			}
			denied := WithSingleModelRequest(context.Background(), func() error { return context.Canceled })
			if _, err := provider.TestLLM(denied, "denied"); err == nil {
				t.Fatal("checkpoint ignored")
			}
			if calls.Load() != 1 {
				t.Fatal("denied HTTP issued")
			}
		})
	}
}

func TestModelRequestBudgetPreservesOrdinaryProviderBehavior(t *testing.T) {
	for _, kind := range []string{"responses", "chat_completions", "ollama"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/result" {
					http.Redirect(w, r, "/result", http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch kind {
				case "responses":
					_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"uncertain"}]}]}`))
				case "chat_completions":
					_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"uncertain"}}]}`))
				default:
					_, _ = w.Write([]byte(`{"response":"uncertain"}`))
				}
			}))
			defer server.Close()
			var provider ConnectionTester
			switch kind {
			case "responses":
				provider = NewOpenAIProvider(server.URL, "test", "", 2)
			case "chat_completions":
				provider = NewOpenAILegacyProvider(server.URL, "test", "", 2)
			default:
				provider = NewOllamaProvider(server.URL, "test", 2)
			}
			// Existing callers still follow redirects and can make independent calls.
			for range 2 {
				if result, err := provider.TestLLM(context.Background(), "label only"); err != nil || result != "uncertain" {
					t.Fatalf("ordinary response: %q, %v", result, err)
				}
			}
			if calls.Load() != 4 {
				t.Fatal("ordinary provider behavior changed", calls.Load())
			}
		})
	}
}

func TestBudgetedModelUpstreamFailureDoesNotRetry(t *testing.T) {
	for _, kind := range []string{"responses", "chat_completions", "ollama"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			var provider ConnectionTester
			switch kind {
			case "responses":
				provider = NewOpenAIProvider(server.URL, "test", "", 2)
			case "chat_completions":
				provider = NewOpenAILegacyProvider(server.URL, "test", "", 2)
			default:
				provider = NewOllamaProvider(server.URL, "test", 2)
			}
			ctx := WithSingleModelRequest(context.Background(), func() error { return nil })
			for range 2 {
				if _, err := provider.TestLLM(ctx, "label only"); err == nil {
					t.Fatal("upstream failure/reissue accepted")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("unaccounted retry", calls.Load())
			}
		})
	}
}

// REFUSED_STREAM normally makes Go's HTTP/2 transport replay a POST with GetBody.
// The wire server refuses consumed requests, so this catches retries below Do.
func TestBudgetedModelDoesNotReplayHTTP2RefusedStream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewUnstartedServer(nil)
	server.EnableHTTP2 = true
	server.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){
		"h2": func(_ *http.Server, conn *tls.Conn, _ http.Handler) {
			defer conn.Close()
			preface := make([]byte, 24)
			if _, err := io.ReadFull(conn, preface); err != nil {
				return
			}
			writeFrame := func(kind, flags byte, stream uint32, payload []byte) error {
				header := []byte{byte(len(payload) >> 16), byte(len(payload) >> 8), byte(len(payload)), kind, flags, 0, 0, 0, 0}
				binary.BigEndian.PutUint32(header[5:], stream)
				_, err := conn.Write(append(header, payload...))
				return err
			}
			if err := writeFrame(4, 0, 0, nil); err != nil { // SETTINGS
				return
			}
			for {
				header := make([]byte, 9)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				length := int(header[0])<<16 | int(header[1])<<8 | int(header[2])
				if _, err := io.CopyN(io.Discard, conn, int64(length)); err != nil {
					return
				}
				kind, flags := header[3], header[4]
				if kind == 4 && flags&1 == 0 {
					if err := writeFrame(4, 1, 0, nil); err != nil {
						return
					}
				}
				if (kind == 0 || kind == 1) && flags&1 != 0 { // END_STREAM
					calls.Add(1)
					if err := writeFrame(3, 0, binary.BigEndian.Uint32(header[5:]), []byte{0, 0, 0, 7}); err != nil { // REFUSED_STREAM
						return
					}
				}
			}
		},
	}
	server.StartTLS()
	defer server.Close()
	provider := NewOpenAIProvider(server.URL, "test", "", 1)
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.ForceAttemptHTTP2 = true
	defer transport.CloseIdleConnections()
	provider.client.Transport = transport
	ctx := WithSingleModelRequest(context.Background(), func() error { return nil })
	if _, err := provider.TestLLM(ctx, "label only"); err == nil {
		t.Fatal("refused request succeeded")
	}
	if calls.Load() != 1 {
		t.Fatal("HTTP/2 transport replayed a budgeted POST", calls.Load())
	}
}
