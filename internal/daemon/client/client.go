package daemonclient

import (
	"context"
	"net"
	"net/http"

	"github.com/anish/claude-msg-utils/internal/paths"
)

// PgnHTTPClient is an HTTP client preconfigured to talk to the pigeon daemon
// over its Unix domain socket.
type PgnHTTPClient struct {
	http.Client
}

// DefaultPgnHTTPClient is a preconfigured PgnHTTPClient that connects to the
// daemon socket.
var DefaultPgnHTTPClient = &PgnHTTPClient{
	Client: http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", paths.SocketPath())
			},
		},
	},
}
