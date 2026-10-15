//go:build !wasip1

package subakofn

import "github.com/modelcontextprotocol/go-sdk/mcp"

// Transport is MCP's stdio transport, so a native build serves any MCP client.
func Transport() mcp.Transport { return &mcp.StdioTransport{} }
