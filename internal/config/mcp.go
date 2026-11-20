package config

import (
	"errors"
	"net"
	"net/url"
)

// Origin returns the single configured MCP authority. Empty disables the endpoint.
// Plaintext requires explicit opt-in and a literal private/loopback host (or localhost).
func (c MCPConfig) Origin() (*url.URL, error) {
	if c.PublicURL == "" {
		return nil, nil
	}
	invalid := errors.New("MCP requires an HTTPS origin, or explicitly enabled private-IP LAN HTTP")
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, invalid
	}
	if u.Scheme == "https" {
		return u, nil
	}
	ip := net.ParseIP(u.Hostname())
	private := u.Hostname() == "localhost" || ip != nil && (ip.IsLoopback() || ip.IsPrivate())
	if u.Scheme != "http" || !c.AllowLANHTTP || !private {
		return nil, invalid
	}
	return u, nil
}
