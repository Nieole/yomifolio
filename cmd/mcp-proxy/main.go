// Command mcp-proxy bridges stdio JSON-RPC to the running library service.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yomifolio/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	flags := flag.NewFlagSet("mcp-proxy", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	origin := flags.String("url", "", "Running service origin (HTTPS, or explicitly allowed private-IP HTTP); /mcp is appended")
	lan := flags.Bool("allow-lan-http", false, "Allow private-IP/localhost plaintext HTTP (credentials can be intercepted)")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return errors.New("invalid proxy options")
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected proxy arguments")
	}
	u, err := (config.MCPConfig{PublicURL: *origin, AllowLANHTTP: *lan}).Origin()
	if err != nil || u == nil {
		return errors.New("provide an HTTPS service origin, or explicitly allow private-IP LAN HTTP")
	}
	token := os.Getenv("YOMIFOLIO_MCP_TOKEN")
	_ = os.Unsetenv("YOMIFOLIO_MCP_TOKEN")
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return errors.New("YOMIFOLIO_MCP_TOKEN must contain an independent MCP credential")
	}
	u.Path = "/mcp"
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Never route independent credentials through environment-selected proxies.
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	var output sync.Mutex
	emit := func(value any) {
		output.Lock()
		defer output.Unlock()
		if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
			cancel()
		}
	}
	fail := func(id json.RawMessage, code int, message string) {
		if len(id) == 0 {
			id = json.RawMessage("null")
		}
		emit(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   any             `json:"error"`
		}{"2.0", id, struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}{code, message}})
	}
	var protocol atomic.Value
	protocol.Store("2025-11-25")
	slots := make(chan struct{}, 8)
	done := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			line := bytes.Clone(scanner.Bytes())
			var envelope struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Method  string          `json:"method"`
			}
			if !json.Valid(line) {
				fail(nil, -32700, "Invalid JSON")
				continue
			}
			if json.Unmarshal(line, &envelope) != nil || envelope.JSONRPC != "2.0" || envelope.Method == "" {
				fail(nil, -32600, "Invalid JSON-RPC request")
				continue
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				done <- nil
				return
			default:
				// Admission must not block stdin EOF detection. Keep no waiting queue
				// or goroutine per excess request; notifications cannot receive errors.
				if len(envelope.ID) > 0 {
					fail(envelope.ID, -32000, "MCP proxy overloaded")
				}
				continue
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-slots }()
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(line))
				if err != nil {
					return
				}
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Accept", "application/json, text/event-stream")
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("MCP-Protocol-Version", protocol.Load().(string))
				response, err := client.Do(request)
				if err != nil {
					if len(envelope.ID) > 0 {
						fail(envelope.ID, -32000, "MCP upstream unavailable")
					}
					return
				}
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusAccepted {
					if len(envelope.ID) > 0 {
						message := "MCP upstream request failed"
						if response.StatusCode == 401 || response.StatusCode == 403 {
							message = "MCP upstream authorization denied"
						}
						fail(envelope.ID, -32000, message)
					}
					return
				}
				if len(envelope.ID) == 0 {
					return
				}
				body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
				if err != nil || len(body) > 8<<20 || !json.Valid(body) {
					fail(envelope.ID, -32000, "Invalid or oversized MCP upstream response")
					return
				}
				if envelope.Method == "initialize" {
					var initialized struct {
						Result struct {
							ProtocolVersion string `json:"protocolVersion"`
						} `json:"result"`
					}
					if json.Unmarshal(body, &initialized) == nil && initialized.Result.ProtocolVersion != "" {
						protocol.Store(initialized.Result.ProtocolVersion)
					}
				}
				emit(json.RawMessage(body))
			}()
		}
		if scanner.Err() != nil {
			done <- errors.New("invalid or oversized stdio input")
			return
		}
		done <- nil
	}()
	var result error
	select {
	case result = <-done:
	case <-ctx.Done():
		_ = os.Stdin.Close()
		result = <-done
	}
	// EOF/disconnect ends only transport requests, never sends business cancellation.
	cancel()
	workers.Wait()
	return result
}
