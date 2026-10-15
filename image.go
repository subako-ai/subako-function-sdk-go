package subakofn

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ImageFile shows the model the image at path, an absolute path under
// /volume. Core reads the file once the call returns, so it must still be
// there then.
func ImageFile(file string) *mcp.ResourceLink {
	return &mcp.ResourceLink{URI: volumeURI(file), Name: path.Base(file)}
}

// ImageResult is a tool's result reading out as JSON, followed by the images
// at paths. Return it beside out, which the SDK sets as the structured
// content:
//
//	res, err := subakofn.ImageResult(plot, plot.Path)
//	return res, plot, err
func ImageResult(out any, paths ...string) (*mcp.CallToolResult, error) {
	text, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("output is not JSON: %w", err)
	}
	content := []mcp.Content{&mcp.TextContent{Text: string(text)}}
	for _, p := range paths {
		content = append(content, ImageFile(p))
	}
	return &mcp.CallToolResult{Content: content}, nil
}

// volumeURI names path as volume:/// and its path beneath /volume, each
// segment percent-encoded; Core shows anything outside the volume as a line
// naming the link. The Rust SDK's volume_uri spells the same URIs.
func volumeURI(file string) string {
	inside, ok := strings.CutPrefix(file, "/volume/")
	if !ok {
		return "file://" + encodePath(file)
	}
	return "volume:///" + encodePath(inside)
}

func encodePath(p string) string {
	segments := strings.Split(p, "/")
	for i, segment := range segments {
		segments[i] = encodeSegment(segment)
	}
	return strings.Join(segments, "/")
}

// encodeSegment keeps ASCII letters, digits and -._~ and percent-encodes
// every other byte.
func encodeSegment(segment string) string {
	var b strings.Builder
	for _, c := range []byte(segment) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
