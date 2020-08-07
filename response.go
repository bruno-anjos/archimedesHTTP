// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP Response reading and parsing.

package http

import (
	"bufio"
	originalHttp "net/http"
)

var ErrNoLocation = originalHttp.ErrNoLocation

type Response = originalHttp.Response

func ReadResponse(r *bufio.Reader, req *Request) (*Response, error) {
	return originalHttp.ReadResponse(r, req)
}