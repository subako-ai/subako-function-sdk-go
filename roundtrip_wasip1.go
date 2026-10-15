package subakofn

import "net/http"

func init() { http.DefaultTransport = RoundTripper{} }
