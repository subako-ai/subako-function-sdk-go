// A Subako function package with three tools: typed input and output, an HTTP
// call through net/http, and an image.
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	subakofn "github.com/subako-ai/subako-function-sdk-go"
)

type ForecastInput struct {
	City string `json:"city" jsonschema:"the city to forecast"`
}

type Forecast struct {
	City  string  `json:"city"`
	HighC float64 `json:"high_c"`
}

func forecast(ctx context.Context, req *mcp.CallToolRequest, in ForecastInput) (*mcp.CallToolResult, Forecast, error) {
	return nil, Forecast{City: in.City, HighC: 21.5}, nil
}

type FetchInput struct {
	URL string `json:"url" jsonschema:"an https URL"`
}

type Page struct {
	Status int    `json:"status"`
	Text   string `json:"text"`
}

func fetch(ctx context.Context, req *mcp.CallToolRequest, in FetchInput) (*mcp.CallToolResult, Page, error) {
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, in.URL, nil)
	if err != nil {
		return nil, Page{}, err
	}
	res, err := http.DefaultClient.Do(get)
	if err != nil {
		return nil, Page{}, err
	}
	defer res.Body.Close()
	text, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, Page{}, err
	}
	return nil, Page{Status: res.StatusCode, Text: string(text)}, nil
}

type Chart struct {
	Path string `json:"path"`
}

// A 1x1 PNG.
const pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func plot(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, Chart, error) {
	chart := Chart{Path: "/volume/tmp/chart.png"}
	png, _ := base64.StdEncoding.DecodeString(pixel)
	if err := os.MkdirAll("/volume/tmp", 0o755); err != nil {
		return nil, Chart{}, err
	}
	if err := os.WriteFile(chart.Path, png, 0o644); err != nil {
		return nil, Chart{}, fmt.Errorf("write the chart: %w", err)
	}
	res, err := subakofn.ImageResult(chart, chart.Path)
	return res, chart, err
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "weather", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "forecast", Description: "Tomorrow's forecast for a city."}, forecast)
	mcp.AddTool(server, &mcp.Tool{Name: "fetch", Description: "Fetch a page's text."}, fetch)
	mcp.AddTool(server, &mcp.Tool{Name: "plot", Description: "Draw a chart and show it."}, plot)
	subakofn.Serve(server)
}
