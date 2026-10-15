package subakofn

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// RoundTripper sends net/http requests through the worker, as [Do] does. A
// wasip1 build installs it as http.DefaultTransport, since the module has no
// sockets of its own.
//
// Bodies travel as text, so a binary request or response body fails with an
// [*HTTPError]; send or save it as a volume file with [Do].
type RoundTripper struct{}

func (RoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	sent, err := request(req)
	if req.Body != nil {
		req.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	res, err := Do(sent)
	if err != nil {
		return nil, err
	}
	header := make(http.Header, len(res.Headers))
	for name, value := range res.Headers {
		header.Set(name, value)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", res.Status, http.StatusText(res.Status)),
		StatusCode:    res.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(res.Body.Text)),
		ContentLength: int64(len(res.Body.Text)),
		Request:       req,
	}, nil
}

func request(req *http.Request) (Request, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	sent := Request{Method: method, URL: req.URL.String()}
	if len(req.Header) > 0 {
		sent.Headers = make(map[string]string, len(req.Header))
		for name, values := range req.Header {
			sent.Headers[strings.ToLower(name)] = strings.Join(values, ", ")
		}
	}
	if req.Body != nil && req.Body != http.NoBody {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return Request{}, err
		}
		if !utf8.Valid(body) {
			return Request{}, &HTTPError{Kind: InvalidRequest, Message: "a binary request body cannot be sent through net/http; send it as a volume file with subakofn.Do"}
		}
		if len(body) > 0 {
			sent.Body = TextBody(string(body))
		}
	}
	if deadline, ok := req.Context().Deadline(); ok {
		left := time.Until(deadline).Milliseconds()
		if left < 1 {
			return Request{}, &HTTPError{Kind: Timeout, Message: "the request's deadline has passed"}
		}
		sent.TimeoutMS = uint64(left)
	}
	return sent, nil
}
