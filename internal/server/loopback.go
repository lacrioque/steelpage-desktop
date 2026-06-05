package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
)

// StartLoopback binds the handler to the given loopback address (use
// "127.0.0.1:0" for an OS-assigned port) and starts serving in the
// background. The concrete URL is known before any request is handled, so
// the webview can be pointed at it immediately. The returned shutdown func
// drains in-flight requests.
func StartLoopback(bind string, h http.Handler) (url string, shutdown func(context.Context) error, err error) {
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return "", nil, fmt.Errorf("bind %s: %w", bind, err)
	}
	srv := &http.Server{Handler: h}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Printf("loopback server: %v\n", err)
		}
	}()
	return "http://" + ln.Addr().String(), srv.Shutdown, nil
}
