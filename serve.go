package subakofn

import (
	"context"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Serve answers MCP requests on stdin until it ends, then returns. A server
// that fails exits the program with status 1.
func Serve(server *mcp.Server) {
	if err := server.Run(context.Background(), Transport()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
