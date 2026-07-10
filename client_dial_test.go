package rdp

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// TestDialUsesCustomDialContext verifies dial() routes through Options.DialContext
// with the target address, instead of a direct TCP dial.
func TestDialUsesCustomDialContext(t *testing.T) {
	local, remote := net.Pipe()
	defer local.Close()
	defer remote.Close()

	var gotNetwork, gotAddr string
	opts := DefaultOptions()
	opts.Host = "10.0.0.7"
	opts.Port = 3390
	opts.DialContext = func(_ context.Context, network, addr string) (net.Conn, error) {
		gotNetwork, gotAddr = network, addr
		return local, nil
	}

	c, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	conn, err := c.dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if conn != local {
		t.Fatalf("dial returned %v, want the DialContext conn", conn)
	}
	if gotNetwork != "tcp" || gotAddr != "10.0.0.7:3390" {
		t.Fatalf("DialContext called with (%q, %q), want (\"tcp\", \"10.0.0.7:3390\")", gotNetwork, gotAddr)
	}
}

// TestDialDefaultsToDirectTCP verifies dial() falls back to a plain TCP dial
// when no DialContext is set.
func TestDialDefaultsToDirectTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	opts := DefaultOptions()
	opts.Host = "127.0.0.1"
	opts.Port = addr.Port

	c, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	conn, err := c.dial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.Close()
}

// TestConnectSurfacesDialContextError verifies Connect wraps a DialContext
// failure in ErrConnectionFailed and surfaces the cause.
func TestConnectSurfacesDialContextError(t *testing.T) {
	opts := DefaultOptions()
	opts.Host = "example.invalid"
	opts.DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("tunnel down")
	}

	c, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = c.Connect()
	if !errors.Is(err, ErrConnectionFailed) {
		t.Fatalf("Connect error = %v, want ErrConnectionFailed", err)
	}
	if !strings.Contains(err.Error(), "tunnel down") {
		t.Fatalf("Connect error %q does not surface the dial cause", err)
	}
}
