package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"yomifolio/internal/application/delegated"
	"yomifolio/internal/application/enrichment"
	"yomifolio/internal/config"
)

func credentialError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	message := "Credential operation failed"
	if errors.Is(err, delegated.ErrDenied) {
		status = http.StatusForbidden
		message = "Credential access denied"
	}
	if errors.Is(err, delegated.ErrInvalid) {
		status = http.StatusBadRequest
		message = "Invalid restrictions: only narrowing is allowed"
	}
	jsonError(w, status, message)
}
func (c *administrationHTTP) listMCPCredentials(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	out, err := c.delegated.List(r.Context(), user.ID)
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
func decodeRestrictions(w http.ResponseWriter, r *http.Request) (delegated.Restrictions, bool) {
	var in delegated.Restrictions
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		jsonError(w, 400, "Invalid credential restrictions")
		return in, false
	}
	return in, true
}
func (c *administrationHTTP) issueMCPCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, ok := decodeRestrictions(w, r)
	if !ok {
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.delegated.Issue(r.Context(), user.ID, in)
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
func (c *administrationHTTP) narrowMCPCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	in, ok := decodeRestrictions(w, r)
	if !ok {
		return
	}
	user, _ := userFromContext(r.Context())
	out, err := c.delegated.Narrow(r.Context(), user.ID, chi.URLParam(r, "credentialId"), in)
	if err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, out)
}
func (c *administrationHTTP) revokeMCPCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, _ := userFromContext(r.Context())
	if err := c.delegated.Revoke(r.Context(), user.ID, chi.URLParam(r, "credentialId")); err != nil {
		credentialError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"revoked": true})
}

func privateMCPHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// MCP trusts no forwarded Host. A proxy must preserve the configured authority,
// strip client forwarding headers, and be explicitly trusted for TLS/client IP.
func mcpRequestAllowed(r *http.Request, cfg config.Config) bool {
	u, err := cfg.MCP.Origin()
	if err != nil || u == nil || !strings.EqualFold(r.Host, u.Host) {
		return false
	}
	if len(r.Header.Values("Origin")) > 1 {
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != u.Scheme+"://"+u.Host {
		return false
	}
	secure := r.TLS != nil
	trusted := ipInAnyCIDR(net.ParseIP(remoteHost(r.RemoteAddr)), cfg.Server.TrustedProxies)
	if trusted && forwardedRequestProto(r) != "" {
		secure = strings.EqualFold(forwardedRequestProto(r), "https")
	}
	if u.Scheme == "https" {
		return secure
	}
	if u.Scheme != "http" || !cfg.MCP.AllowLANHTTP || !privateMCPHost(u.Hostname()) {
		return false
	}
	// Forwarding must not disguise a public client as a private proxy.
	peer := remoteHost(r.RemoteAddr)
	if trusted {
		forwarded := forwardedClientIP(r)
		if forwarded == "" {
			return false
		}
		peer = forwarded
	}
	return privateMCPHost(peer)
}

func newMCPHandler(service *delegated.Service, enrichmentService *enrichment.Service, manager *config.Manager) http.Handler {
	if service == nil {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		cfg := manager.Snapshot()
		if cfg.MCP.PublicURL == "" {
			http.NotFound(w, r)
			return
		}
		if !mcpRequestAllowed(r, cfg) {
			http.Error(w, "MCP transport access denied", http.StatusForbidden)
			return
		}
		// Cookies, query strings and forwarded credentials never authenticate an agent.
		values := r.Header.Values("Authorization")
		if r.URL.RawQuery != "" || len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
			http.Error(w, "MCP credential required", http.StatusUnauthorized)
			return
		}
		subject, err := service.Authenticate(r.Context(), strings.TrimPrefix(values[0], "Bearer "))
		if err != nil {
			http.Error(w, "MCP credential invalid", http.StatusUnauthorized)
			return
		}
		// Visibility follows the currently granted capabilities; the handlers below keep
		// authorizing their own operations, so a listing is never the access boundary.
		capabilities, err := service.GrantedCapabilities(r.Context(), subject)
		if err != nil {
			http.Error(w, "MCP credential invalid", http.StatusUnauthorized)
			return
		}
		// The strict canonical authority check above replaces SDK localhost-only Host
		// protection, permitting explicitly configured HTTPS reverse proxies.
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			server := mcp.NewServer(&mcp.Implementation{Name: "yomifolio-library", Version: "1"}, nil)
			listOnlyGranted(server, newGrantedTools(capabilities))
			type input struct {
				SeriesID int64 `json:"series_id" jsonschema:"positive series identifier"`
			}
			mcp.AddTool(server, &mcp.Tool{Name: "library_get_series", Description: "Read the identity of one series in an authorized library. Unknown or unauthorized identifiers are denied alike.", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(bool)}}, func(ctx context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, delegated.Series, error) {
				out, err := service.GetSeries(ctx, subject, in.SeriesID)
				if err != nil {
					return nil, delegated.Series{}, delegated.ErrDenied
				}
				return nil, out, nil
			})
			registerDiscoveryTools(server, service, subject, r.Context())
			registerMCPImages(server, service, subject, r.Context())
			registerMCPRuns(server, service, subject)
			registerMCPMatching(server, service, subject)
			if enrichmentService != nil {
				registerMCPEnrichment(server, enrichmentService, subject, r.Context())
				registerMCPCleanup(server, enrichmentService, subject, r.Context())
			}
			registerMCPModelBudgets(server, service, subject)
			return server
		}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		serveBoundedMCP(w, r, handler)
	})
}
