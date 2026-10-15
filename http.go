package subakofn

import (
	"encoding/json"
	"fmt"
)

// Request is an HTTP request the worker sends for the function: https only,
// to hosts the agent's grant allows, with any credential the grant names for
// the origin attached on the way out. Redirects are followed unless
// FollowRedirects says otherwise.
type Request struct {
	// GET unless set: GET, HEAD, POST, PUT, PATCH, DELETE or OPTIONS.
	Method          string            `json:"method,omitempty"`
	URL             string            `json:"url"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            *RequestBody      `json:"body,omitempty"`
	Output          *ResponseOutput   `json:"output,omitempty"`
	FollowRedirects *bool             `json:"follow_redirects,omitempty"`
	TimeoutMS       uint64            `json:"timeout_ms,omitempty"`
}

// RequestBody is one of [TextBody], [JSONBody] or [FileBody].
type RequestBody struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	JSON any    `json:"json,omitempty"`
	Path string `json:"path,omitempty"`
}

// TextBody sends text, as text/plain unless a content-type header says otherwise.
func TextBody(text string) *RequestBody { return &RequestBody{Type: "text", Text: text} }

// JSONBody sends value as JSON, as application/json unless a content-type
// header says otherwise.
func JSONBody(value any) *RequestBody { return &RequestBody{Type: "json", JSON: value} }

// FileBody streams the volume file at path.
func FileBody(path string) *RequestBody { return &RequestBody{Type: "file", Path: path} }

// ResponseOutput is where the response body goes: inline as text by default,
// or into a volume file with [ToFile].
type ResponseOutput struct {
	Type string `json:"type"`
	Path string `json:"path,omitempty"`
}

// ToFile saves the response body to the volume file at path, overwriting it,
// so a large or binary body is bounded only by the call's write budget.
func ToFile(path string) *ResponseOutput { return &ResponseOutput{Type: "file", Path: path} }

// Response is what came back. Header names are lowercase, repeated headers
// joined with ", ".
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    ResponseBody      `json:"body"`
}

// ResponseBody is the body inline as Text, or saved to the volume file at
// Path, Bytes long.
type ResponseBody struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Path  string `json:"path,omitempty"`
	Bytes uint64 `json:"bytes,omitempty"`
}

// HTTPError says why a request produced no response, in a message written for
// the model, so a tool can return it as it is.
type HTTPError struct {
	Kind    HTTPErrorKind `json:"kind"`
	Message string        `json:"message"`
}

func (e *HTTPError) Error() string { return e.Message }

type HTTPErrorKind string

const (
	InvalidRequest        HTTPErrorKind = "invalid_request"
	HostNotAllowed        HTTPErrorKind = "host_not_allowed"
	ForbiddenDestination  HTTPErrorKind = "forbidden_destination"
	CredentialHeader      HTTPErrorKind = "credential_header"
	CredentialUnavailable HTTPErrorKind = "credential_unavailable"
	TooManyRedirects      HTTPErrorKind = "too_many_redirects"
	Timeout               HTTPErrorKind = "timeout"
	// The body is over the inline limit; save it with [ToFile] instead.
	TooLarge HTTPErrorKind = "too_large"
	// The body is not UTF-8; save it with [ToFile] instead.
	NotText     HTTPErrorKind = "not_text"
	WriteBudget HTTPErrorKind = "write_budget"
	BadPath     HTTPErrorKind = "bad_path"
	Connection  HTTPErrorKind = "connection"
	// The import misbehaved, or the program runs outside the worker.
	ABI HTTPErrorKind = "abi"
)

// Get fetches url, its body inline as text.
func Get(url string) (*Response, error) { return Do(Request{URL: url}) }

// Do sends req through the worker. Its error is an [*HTTPError].
func Do(req Request) (*Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, &HTTPError{Kind: InvalidRequest, Message: fmt.Sprintf("request is not JSON: %v", err)}
	}
	answer, err := host(body)
	if err != nil {
		return nil, &HTTPError{Kind: ABI, Message: err.Error()}
	}
	var envelope struct {
		Ok    *Response  `json:"ok"`
		Error *HTTPError `json:"error"`
	}
	if err := json.Unmarshal(answer, &envelope); err != nil {
		return nil, &HTTPError{Kind: ABI, Message: fmt.Sprintf("subako_http answered with no answer: %v", err)}
	}
	switch {
	case envelope.Error != nil:
		return nil, envelope.Error
	case envelope.Ok != nil:
		return envelope.Ok, nil
	default:
		return nil, &HTTPError{Kind: ABI, Message: "subako_http answered with no answer"}
	}
}

// host performs a request through the worker's subako_http import; tests
// replace it.
var host = callHost
