package metadata

import (
	"context"
	"errors"
	"net/http"
	"sync"
)

type modelRequestKey struct{}
type singleModelRequest struct {
	mu    sync.Mutex
	sent  bool
	check func() error
}

// WithSingleModelRequest forbids hidden reissues within one budget admission.
// The guard checks live authority immediately before the actual HTTP request.
// Redirects are disabled: they must not silently repeat a paid POST.
func WithSingleModelRequest(ctx context.Context, check func() error) context.Context {
	return context.WithValue(ctx, modelRequestKey{}, &singleModelRequest{check: check})
}
func modelRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	guard, _ := req.Context().Value(modelRequestKey{}).(*singleModelRequest)
	if guard == nil {
		return client.Do(req)
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.sent {
		return nil, errors.New("model request already admitted")
	}
	if err := guard.check(); err != nil {
		return nil, err
	}
	guard.sent = true
	// Go's HTTP/2 transport can replay POST on REFUSED_STREAM/GOAWAY below
	// Client.Do. Do not supply a body-rewind recipe for a paid admission.
	// Retrying an unused connection before any request is sent remains safe.
	req = req.Clone(req.Context())
	req.GetBody = nil
	limited := *client
	limited.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return limited.Do(req)
}
