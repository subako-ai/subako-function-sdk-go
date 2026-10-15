package subakofn

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestImageFileLinksIntoTheVolume(t *testing.T) {
	for file, uri := range map[string]string{
		"/volume/tmp/chart.png": "volume:///tmp/chart.png",
		"/volume/a b/グラフ.png":   "volume:///a%20b/%E3%82%B0%E3%83%A9%E3%83%95.png",
		"/volume/x/%2F~_-.png":  "volume:///x/%252F~_-.png",
		"/package/logo.png":     "file:///package/logo.png",
	} {
		link := ImageFile(file)
		if link.URI != uri {
			t.Errorf("ImageFile(%q).URI = %q, want %q", file, link.URI, uri)
		}
	}
	if name := ImageFile("/volume/tmp/chart.png").Name; name != "chart.png" {
		t.Errorf("Name = %q, want chart.png", name)
	}
}

func TestImageResultReadsAsJSONThenShowsEachImage(t *testing.T) {
	res, err := ImageResult(map[string]string{"path": "/volume/a.png"}, "/volume/a.png", "/volume/b.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) != 3 {
		t.Fatalf("got %d blocks, want 3", len(res.Content))
	}
	if text := res.Content[0].(*mcp.TextContent).Text; text != `{"path":"/volume/a.png"}` {
		t.Errorf("text = %s", text)
	}
	if uri := res.Content[2].(*mcp.ResourceLink).URI; uri != "volume:///b.png" {
		t.Errorf("second image = %s", uri)
	}
}

func TestImageResultRefusesOutputThatIsNotJSON(t *testing.T) {
	if _, err := ImageResult(func() {}); err == nil {
		t.Fatal("want an error")
	}
}
