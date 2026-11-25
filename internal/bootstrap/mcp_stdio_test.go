package bootstrap_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/bootstrap"
	"yomifolio/internal/config"
	"yomifolio/internal/database"
)

func TestMCPStdioProductionProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	store, cfg := fixture(t)
	process, err := bootstrap.Open(ctx, store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(`{"username":"admin","password":"password123"}`)))
	if setup.Code != 200 {
		t.Fatal(setup.Body)
	}
	var auth struct {
		CSRF string `json:"csrf_token"`
	}
	_ = json.Unmarshal(setup.Body.Bytes(), &auth)
	admin := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(setup.Result().Cookies()[0])
		r.Header.Set("X-CSRF-Token", auth.CSRF)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("admin %d %s", w.Code, w.Body)
		}
		return w
	}
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Proxy", Path: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Through production", Path: lib.Path + "/series"})
	if err != nil {
		t.Fatal(err)
	}
	issued := admin("POST", "/api/mcp/credentials", fmt.Sprintf(`{"user_id":1,"name":"stdio","library_ids":[%d],"capabilities":["read","cleanup"]}`, lib.ID))
	var credential struct{ Token, ID string }
	_ = json.Unmarshal(issued.Body.Bytes(), &credential)
	var stall atomic.Bool
	entered, released := make(chan struct{}, 1), make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stall.Load() {
			_, _ = io.Copy(io.Discard, r.Body)
			entered <- struct{}{}
			<-r.Context().Done()
			released <- struct{}{}
			return
		}
		router.ServeHTTP(w, r)
	}))
	defer server.Close()
	snapshot := cfg.Snapshot()
	snapshot.MCP.PublicURL = server.URL
	snapshot.MCP.AllowLANHTTP = true
	cfg.Replace(&snapshot)
	connect := func(token string) (*mcp.ClientSession, *exec.Cmd, *bytes.Buffer, error) {
		cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+token)
		stderr := new(bytes.Buffer)
		cmd.Stderr = stderr
		client := mcp.NewClient(&mcp.Implementation{Name: "stdio-acceptance", Version: "1"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
		return session, cmd, stderr, err
	}
	for i := 0; i < 2; i++ {
		session, cmd, stderr, err := connect(credential.Token)
		if err != nil {
			t.Fatalf("connect: %v %s", err, stderr)
		}
		tools, err := session.ListTools(ctx, nil)
		if err != nil || len(tools.Tools) == 0 {
			t.Fatalf("tools: %+v %v", tools, err)
		}
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": series.ID}})
		if err != nil || result.IsError {
			t.Fatalf("call: %+v %v", result, err)
		}
		assertImageEvidence(t, session, store, lib.ID, series.ID, lib.Path, config.ThumbnailDir(cfg.Snapshot()))
		server.CloseClientConnections()
		// A broken in-flight transport may fail once; never replay arbitrary tools.
		_, _ = session.ListTools(ctx, nil)
		if _, err := session.ListTools(ctx, nil); err != nil {
			t.Fatalf("reconnect after upstream socket loss: %v", err)
		}
		encoded, _ := json.Marshal(result)
		if !strings.Contains(string(encoded), "Through production") {
			t.Fatal(string(encoded))
		}
		if i == 1 {
			admin("PATCH", "/api/mcp/credentials/"+credential.ID, fmt.Sprintf(`{"user_id":1,"name":"stdio","library_ids":[%d],"capabilities":["cleanup"]}`, lib.ID))
			// The narrowed credential no longer lists the read tools, so the call is refused at the
			// protocol boundary instead of reaching a denied handler.
			if listed, e := session.ListTools(ctx, nil); e != nil || offersTool(listed, "library_get_series") {
				t.Fatalf("narrowed listing: %+v %v", listed, e)
			}
			denied, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "library_get_series", Arguments: map[string]any{"series_id": series.ID}})
			if err == nil && !denied.IsError {
				t.Fatalf("narrow: %+v %v", denied, err)
			}
			admin("DELETE", "/api/mcp/credentials/"+credential.ID, "")
			if _, err := session.ListTools(ctx, nil); err == nil {
				t.Fatal("revocation not immediate")
			}
		}
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Success() {
			t.Fatalf("EOF did not exit cleanly: %v %s", cmd.ProcessState, stderr)
		}
		if strings.Contains(stderr.String(), credential.Token) {
			t.Fatal("secret in stderr")
		}
	}
	t.Run("malformed-and-EOF", func(t *testing.T) {
		cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+credential.Token)
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		stderr := new(bytes.Buffer)
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(input, "not-json"); err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(output)
		if !scanner.Scan() || !strings.Contains(scanner.Text(), `"code":-32700`) || !json.Valid(scanner.Bytes()) {
			t.Fatalf("malformed response %q", scanner.Text())
		}
		_ = input.Close()
		if err := cmd.Wait(); err != nil {
			t.Fatalf("EOF: %v %s", err, stderr)
		}
	})
	t.Run("EOF-cancels-transport-not-business", func(t *testing.T) {
		stall.Store(true)
		defer stall.Store(false)
		cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN="+credential.Token)
		input, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		cmd.Stdout = &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_, err = fmt.Fprintln(input, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("request never reached upstream")
		}
		_ = input.Close()
		if err := cmd.Wait(); err != nil {
			t.Fatalf("EOF exit: %v", err)
		}
		select {
		case <-released:
		case <-ctx.Done():
			t.Fatal("upstream connection leaked")
		}
	})
	t.Run("unsafe-plaintext", func(t *testing.T) {
		cmd := exec.CommandContext(ctx, binary, "--url", "http://8.8.8.8", "--allow-lan-http")
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN=must-not-leak")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil || stdout.Len() != 0 || strings.Contains(stderr.String(), "must-not-leak") {
			t.Fatalf("unsafe launch: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
		}
	})
	t.Run("upstream-unavailable", func(t *testing.T) {
		dead := httptest.NewServer(router)
		endpoint := dead.URL
		dead.Close()
		cmd := exec.CommandContext(ctx, binary, "--url", endpoint, "--allow-lan-http")
		cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN=must-not-leak")
		client := mcp.NewClient(&mcp.Implementation{Name: "failure", Version: "1"}, nil)
		session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
		if err == nil {
			session.Close()
			t.Fatal("unavailable upstream accepted")
		}
		if cmd.ProcessState == nil {
			t.Fatal("failed initialization leaked process")
		}
	})
	if session, _, stderr, err := connect("invalid-secret"); err == nil {
		session.Close()
		t.Fatal("invalid credential accepted")
	} else if strings.Contains(stderr.String(), "invalid-secret") {
		t.Fatal("secret leaked")
	}
}

func TestMCPStdioSaturatedEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "mcp-proxy.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "../../cmd/mcp-proxy")
	buildStarted := time.Now()
	out, buildErr := build.CombinedOutput()
	t.Logf("proxy build: elapsed=%s context=%v err=%v output=%q", time.Since(buildStarted), ctx.Err(), buildErr, out)
	if buildErr != nil {
		t.FailNow()
	}
	// Hold transport requests open; no application run or business cancellation is involved.
	entered, released := make(chan string, 32), make(chan struct{}, 32)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var request struct{ Method string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- request.Method
		<-r.Context().Done()
		released <- struct{}{}
	}))
	defer server.Close()
	cmd := exec.CommandContext(ctx, binary, "--url", server.URL, "--allow-lan-http")
	cmd.Env = append(os.Environ(), "YOMIFOLIO_MCP_TOKEN=saturation-secret")
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(waited)
	}()
	defer func() {
		cancel()
		_ = input.Close()
		<-waited
	}()
	for id := 1; id <= 8; id++ {
		if _, err := fmt.Fprintf(input, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"method\":\"tools/list\"}\n", id); err != nil {
			t.Fatal(err)
		}
		select {
		case method := <-entered:
			if method != "tools/list" {
				t.Fatalf("unexpected upstream method %q", method)
			}
		case <-ctx.Done():
			t.Fatal("did not saturate upstream concurrency")
		}
	}
	if _, err := fmt.Fprintln(input, `{"jsonrpc":"2.0","id":9,"method":"tools/list"}`); err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	select {
	case <-waited:
		if waitErr != nil {
			t.Fatalf("EOF exit: %v stderr=%s", waitErr, &stderr)
		}
	case <-time.After(3 * time.Second):
		cancel()
		<-waited
		t.Fatal("saturated admission prevented prompt EOF exit")
	}
	for range 8 {
		select {
		case <-released:
		case <-ctx.Done():
			t.Fatal("upstream transport connection leaked")
		}
	}
	select {
	case method := <-entered:
		t.Fatalf("overload or disconnect sent an extra upstream request: %q", method)
	default:
	}
	scanner := bufio.NewScanner(&stdout)
	overloaded := false
	for scanner.Scan() {
		var response struct {
			JSONRPC string `json:"jsonrpc"`
			ID      int    `json:"id"`
			Error   struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil || response.JSONRPC != "2.0" {
			t.Fatalf("non-protocol stdout: %q", scanner.Text())
		}
		if response.ID == 9 && response.Error.Code == -32000 && response.Error.Message == "MCP proxy overloaded" {
			overloaded = true
		}
	}
	if err := scanner.Err(); err != nil || !overloaded {
		t.Fatalf("missing explicit overload rejection: err=%v", err)
	}
	if strings.Contains(stderr.String(), "saturation-secret") {
		t.Fatal("secret in diagnostics")
	}
}
