package provider

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phron-net/api/gen/zonhoren/phron/v1/phronv1connect"
)

func init() {
	// Keep the tests fast: the waits poll in milliseconds.
	pollInterval = time.Millisecond
	retryDelay = time.Millisecond
}

// newTestClients serves the given handlers over httptest and returns clients
// that talk to them with the same JSON Connect protocol the provider uses.
func newTestClients(t *testing.T, srv phronv1connect.ServerServiceHandler, stg phronv1connect.StorageServiceHandler) *Clients {
	t.Helper()
	mux := http.NewServeMux()
	if srv != nil {
		p, h := phronv1connect.NewServerServiceHandler(srv)
		mux.Handle(p, h)
	}
	if stg != nil {
		p, h := phronv1connect.NewStorageServiceHandler(stg)
		mux.Handle(p, h)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return NewClients(ts.Client(), ts.URL)
}

var errFake = errors.New("fake api error")
