// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP client. See RFC 7230 through 7235.
//
// This is the high-level Client interface.
// The low-level implementation is in transport.go.

package http

import (
	"io"
	originalHttp "net/http"
	"net/url"
	"strings"

	archimedes "github.com/bruno-anjos/archimedes/api"
	log "github.com/sirupsen/logrus"
)

type Client struct {
	originalHttp.Client
}

var ErrUseLastResponse = originalHttp.ErrUseLastResponse

var DefaultClient = &Client{}

type RoundTripper = originalHttp.RoundTripper

func Get(url string) (resp *Response, err error) {
	return DefaultClient.Get(url)
}
func (c *Client) Get(url string) (resp *Response, err error) {
	req, err := originalHttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c *Client) Do(req *Request) (*Response, error) {
	log.Debug("host in Do: ", req.Host)
	resolvedHostPort, err := c.resolveServiceInArchimedes(req.Host)
	if err != nil {
		panic(err)
	}

	oldUrl := req.URL
	newUrl := url.URL{
		Scheme:     oldUrl.Scheme,
		Opaque:     oldUrl.Opaque,
		User:       oldUrl.User,
		Host:       resolvedHostPort,
		Path:       oldUrl.Path,
		RawPath:    oldUrl.RawPath,
		ForceQuery: oldUrl.ForceQuery,
		RawQuery:   oldUrl.RawQuery,
		Fragment:   oldUrl.Fragment,
	}

	req, err = originalHttp.NewRequest(req.Method, newUrl.String(), req.Body)
	if err != nil {
		panic(err)
	}

	resp, err := c.Client.Do(req)

	return resp, err
}

// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
func (c *Client) resolveServiceInArchimedes(hostPort string) (resolvedHostPort string, err error) {
	return archimedes.ResolveServiceInArchimedes(&c.Client, hostPort)
}

// Post issues a POST to the specified URL.
//
// Caller should close resp.Body when done reading from it.
//
// If the provided body is an io.Closer, it is closed after the
// request.
//
// To set custom headers, use NewRequest and Client.Do.
//
// See the Client.Do method documentation for details on how redirects
// are handled.
func (c *Client) Post(url, contentType string, body io.Reader) (resp *Response, err error) {
	req, err := originalHttp.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	return c.Do(req)
}

// PostForm issues a POST to the specified URL,
// with data's keys and values URL-encoded as the request body.
//
// The Content-Type header is set to application/x-www-form-urlencoded.
// To set other headers, use NewRequest and Client.Do.
//
// When err is nil, resp always contains a non-nil resp.Body.
// Caller should close resp.Body when done reading from it.
//
// See the Client.Do method documentation for details on how redirects
// are handled.
func (c *Client) PostForm(url string, data url.Values) (resp *Response, err error) {
	return c.Post(url, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
}

// Head issues a HEAD to the specified URL. If the response is one of the
// following redirect codes, Head follows the redirect after calling the
// Client's CheckRedirect function:
//
//    301 (Moved Permanently)
//    302 (Found)
//    303 (See Other)
//    307 (Temporary Redirect)
//    308 (Permanent Redirect)
func (c *Client) Head(url string) (resp *Response, err error) {
	req, err := originalHttp.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func Head(url string) (resp *Response, err error) {
	return DefaultClient.Head(url)
}

func Post(url, contentType string, body io.Reader) (resp *Response, err error) {
	return DefaultClient.Post(url, contentType, body)
}

func PostForm(url string, data url.Values) (resp *Response, err error) {
	return DefaultClient.PostForm(url, data)
}
