package subakofn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// answering makes the worker's import answer every request with answer,
// keeping what was sent in sent.
func answering(t *testing.T, answer string, sent *map[string]any) {
	t.Helper()
	before := host
	host = func(body []byte) ([]byte, error) {
		if sent != nil {
			if err := json.Unmarshal(body, sent); err != nil {
				t.Fatal(err)
			}
		}
		return []byte(answer), nil
	}
	t.Cleanup(func() { host = before })
}

func TestDoSendsTheRequestAndReadsTheResponse(t *testing.T) {
	var sent map[string]any
	answering(t, `{"ok":{"status":201,"headers":{"x-a":"1"},"body":{"type":"inline","text":"made"}}}`, &sent)
	res, err := Do(Request{Method: "POST", URL: "https://api.example.com/things", Body: JSONBody(map[string]int{"n": 1})})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 201 || res.Body.Text != "made" || res.Headers["x-a"] != "1" {
		t.Errorf("response = %+v", res)
	}
	want := `{"body":{"json":{"n":1},"type":"json"},"method":"POST","url":"https://api.example.com/things"}`
	if got, _ := json.Marshal(sent); string(got) != want {
		t.Errorf("sent %s, want %s", got, want)
	}
}

func TestDoReturnsTheWorkersRefusal(t *testing.T) {
	answering(t, `{"error":{"kind":"host_not_allowed","message":"this function may not reach x.test"}}`, nil)
	_, err := Get("https://x.test/")
	var refused *HTTPError
	if !errors.As(err, &refused) || refused.Kind != HostNotAllowed || err.Error() != "this function may not reach x.test" {
		t.Errorf("err = %v", err)
	}
}

func TestDoOutsideTheWorkerFails(t *testing.T) {
	_, err := Get("https://example.com/")
	var refused *HTTPError
	if !errors.As(err, &refused) || refused.Kind != ABI {
		t.Errorf("err = %v", err)
	}
}

func TestNetHTTPGoesThroughTheWorker(t *testing.T) {
	var sent map[string]any
	answering(t, `{"ok":{"status":200,"headers":{"content-type":"application/json"},"body":{"type":"inline","text":"{\"ok\":true}"}}}`, &sent)
	client := &http.Client{Transport: RoundTripper{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, "https://api.example.com/v1?q=1", strings.NewReader("hello"))
	req.Header.Add("X-Tag", "a")
	req.Header.Add("X-Tag", "b")
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || string(body) != `{"ok":true}` || res.Header.Get("Content-Type") != "application/json" {
		t.Errorf("response = %d %q %v", res.StatusCode, body, res.Header)
	}
	if sent["method"] != "PUT" || sent["url"] != "https://api.example.com/v1?q=1" {
		t.Errorf("sent %v", sent)
	}
	if headers := sent["headers"].(map[string]any); headers["x-tag"] != "a, b" {
		t.Errorf("headers = %v", headers)
	}
	if body := sent["body"].(map[string]any); body["type"] != "text" || body["text"] != "hello" {
		t.Errorf("body = %v", body)
	}
	if ms := sent["timeout_ms"].(float64); ms < 50_000 || ms > 60_000 {
		t.Errorf("timeout_ms = %v", ms)
	}
}

func TestNetHTTPRefusesABinaryBody(t *testing.T) {
	answering(t, `{"ok":{"status":200,"headers":{},"body":{"type":"inline","text":""}}}`, nil)
	client := &http.Client{Transport: RoundTripper{}}
	_, err := client.Post("https://api.example.com/", "application/octet-stream", strings.NewReader("\xff\xfe"))
	var refused *HTTPError
	if !errors.As(err, &refused) || refused.Kind != InvalidRequest {
		t.Errorf("err = %v", err)
	}
}
