// Package subakofn runs a server built with the official MCP Go SDK as a
// Subako function module, and reaches what the function worker offers it.
//
// Build the module with GOOS=wasip1 GOARCH=wasm. [Serve] answers the worker's
// requests; http.DefaultTransport sends requests through the worker, so
// net/http and the clients built on it work as they are; [ImageFile] and
// [ImageResult] show the model an image from the session volume.
package subakofn
