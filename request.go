// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP Request reading and parsing.

package http

import (
	"bufio"
	"context"
	"io"
	originalHttp "net/http"
)

var (
	ErrNotSupported         = originalHttp.ErrNotSupported
	ErrUnexpectedTrailer    = originalHttp.ErrUnexpectedTrailer
	ErrMissingBoundary      = originalHttp.ErrMissingBoundary
	ErrNotMultipart         = originalHttp.ErrNotMultipart
	ErrHeaderTooLong        = originalHttp.ErrHeaderTooLong
	ErrShortBody            = originalHttp.ErrShortBody
	ErrMissingContentLength = originalHttp.ErrMissingContentLength
)

var ErrMissingFile = originalHttp.ErrMissingFile

type ProtocolError = originalHttp.ProtocolError

type Request = originalHttp.Request

// ErrNoCookie is returned by Request's Cookie method when a cookie is not found.
var ErrNoCookie = originalHttp.ErrNoCookie

func ParseHTTPVersion(vers string) (major, minor int, ok bool) {
	return originalHttp.ParseHTTPVersion(vers)
}

func NewRequest(method, url string, body io.Reader) (*Request, error) {
	return originalHttp.NewRequest(method, url, body)
}

func NewRequestWithContext(ctx context.Context, method, url string, body io.Reader) (*Request, error) {
	return originalHttp.NewRequestWithContext(ctx, method, url, body)
}

func ReadRequest(b *bufio.Reader) (*Request, error) {
	return originalHttp.ReadRequest(b)
}

func MaxBytesReader(w ResponseWriter, r io.ReadCloser, n int64) io.ReadCloser {
	return MaxBytesReader(w, r, n)
}
