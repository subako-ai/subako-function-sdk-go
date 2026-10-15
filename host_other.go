//go:build !wasip1

package subakofn

import "errors"

func callHost([]byte) ([]byte, error) {
	return nil, errors.New("subako_http exists only inside the function worker")
}
