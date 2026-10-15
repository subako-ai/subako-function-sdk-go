package subakofn

import (
	"fmt"
	"unsafe"
)

//go:wasmimport subako_http request
func hostRequest(ptr unsafe.Pointer, len uint32) int32

//go:wasmimport subako_http read_response
func hostReadResponse(ptr unsafe.Pointer, len uint32) int32

func callHost(body []byte) ([]byte, error) {
	n := hostRequest(unsafe.Pointer(unsafe.SliceData(body)), uint32(len(body)))
	if n < 0 {
		return nil, fmt.Errorf("subako_http returned %d", n)
	}
	answer := make([]byte, n)
	if code := hostReadResponse(unsafe.Pointer(unsafe.SliceData(answer)), uint32(n)); code != 0 {
		return nil, fmt.Errorf("subako_http returned %d", code)
	}
	return answer, nil
}
