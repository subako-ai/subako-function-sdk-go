# subako-function-sdk-go

Write Subako function packages in Go with the [official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk). Each tool you add becomes a function an agent can call.

```go
func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "github", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "repo", Description: "A GitHub repository's stars, forks and open issues."}, repo)
	subakofn.Serve(server)
}
```

- `subakofn.Serve` replaces `server.Run(ctx, &mcp.StdioTransport{})`, which stalls under WASI.
- `net/http` works as usual; requests go through the worker, which applies the function's network and credentials. `subakofn.Do` reaches what net/http cannot, such as volume files as bodies.
- `subakofn.ImageResult` and `subakofn.ImageFile` show the model an image from `/volume`.

Build and upload, with `subako-package.json` holding `{"abi": 1, "name": ..., "description": ...}`:

```sh
GOOS=wasip1 GOARCH=wasm go build -o dist/module.wasm .
cp subako-package.json dist/
subako function create dist   # or: subako function push <function-id> dist
```

See [examples/github](examples/github). Requires Go 1.25.

## License

Licensed under either of [Apache License, Version 2.0](LICENSE-APACHE) or [MIT license](LICENSE-MIT) at your option.
