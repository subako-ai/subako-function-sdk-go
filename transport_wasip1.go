package subakofn

import (
	"bytes"
	"context"
	"os"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Transport is MCP's stdio transport, taking one request at a time.
//
// Under wasip1 a read of stdin blocks every goroutine, so the SDK's reader,
// back at stdin, would starve the handler writing the answer the worker
// waits for. Stdin is read again only once every request read so far has
// its response written.
func Transport() mcp.Transport { return turnTransport{} }

type turnTransport struct{}

// How many requests may wait for their responses at once; the worker sends one.
const maxPending = 64

func (turnTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	turn := make(chan struct{}, maxPending)
	conn, err := (&mcp.IOTransport{Reader: &turnReader{turn: turn}, Writer: os.Stdout}).Connect(ctx)
	if err != nil {
		return nil, err
	}
	return turnConn{conn, turn}, nil
}

type turnReader struct {
	turn chan struct{}
	// Requests read and not yet answered, one per line.
	pending int
}

func (r *turnReader) Read(p []byte) (int, error) {
	for ; r.pending > 0; r.pending-- {
		<-r.turn
	}
	n, err := os.Stdin.Read(p)
	r.pending = bytes.Count(p[:n], []byte{'\n'})
	return n, err
}

func (r *turnReader) Close() error { return os.Stdin.Close() }

type turnConn struct {
	mcp.Connection
	turn chan struct{}
}

func (c turnConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	if _, ok := msg.(*jsonrpc.Response); ok {
		select {
		case c.turn <- struct{}{}:
		default:
		}
	}
	return err
}
