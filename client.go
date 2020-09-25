// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP client. See RFC 7230 through 7235.
//
// This is the high-level Client interface.
// The low-level implementation is in transport.go.

package http

import (
	"fmt"
	"io"
	"net"
	originalHttp "net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bruno-anjos/cloud-edge-deployment/pkg/archimedes"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

type (
	AddressCacheKey   = string
	AddressCacheValue = string
)

const (
	refreshCacheTimeout = 30
)

type (
	MiddlewareFunc = func(reqId string, req *Request)

	middlewaresMapKey   = string
	middlewaresMapValue = MiddlewareFunc
)

// Client in order for this client to use archimedes properly, protocols that
// use http for handshake and are stream oriented should behave in two possible
// ways:
//
//	1. if the interaction is stateful it should only restart the connection when
//	it is ready to lose server side state, since archimedes might have cached a
//	different url for the corresponding service, meaning that it will connect to
//	a different server when it is established for the second time, thus losing
//	whatever state was in the server.
//
//	2. if the interaction is stateless it should restart periodically, in order
//	to reflect possible changes archimedes might received. The speed at which the
//	connection is restarted is proportional to the freshness of the host url
//	being used to access a given service.
type Client struct {
	originalHttp.Client
	cache             sync.Map
	refreshingChan    chan struct{}
	beforeMiddlewares sync.Map
	afterMiddlewares  sync.Map
	archimedesClient  *archimedes.Client
}

var ErrUseLastResponse = originalHttp.ErrUseLastResponse

var DefaultClient = &Client{}

type RoundTripper = originalHttp.RoundTripper

func Get(url string) (resp *Response, err error) {
	return DefaultClient.Get(url)
}

// RegisterMiddleware registers a middleware with id midId and a function midFunc that is ran everytime a request
// is done. If afterResolving is true the function is called with the resulting request after resolving the request url
// through archimedes. If afterResolving is false the function is called with the original request.
//
// Even though midFunc receives a pointer to a request, it should only read fields from it and never change them,
// since there are no guarantees on the order the different middlewares will be called.
// The request is only passed as a pointer to avoid making a copy for each middleware.
func (c *Client) RegisterMiddleware(midId string, midFunc MiddlewareFunc, afterResolving bool) {
	var loaded bool
	if afterResolving {
		_, loaded = c.afterMiddlewares.LoadOrStore(midId, midFunc)
	} else {
		_, loaded = c.beforeMiddlewares.LoadOrStore(midId, midFunc)
	}

	if loaded {
		panic(fmt.Sprintf("error registering: middleware with id %s already exists", midId))
	}
	log.Debugf("registered middleware %s", midId)
}

func (c *Client) Get(url string) (resp *Response, err error) {
	req, err := originalHttp.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func (c *Client) refreshCachePeriodically() {
	cacheTicker := time.NewTicker(refreshCacheTimeout * time.Second)

	var err error
	for {
		c.cache.Range(func(key, value interface{}) bool {
			hostPort := key.(AddressCacheKey)
			_, err = c.resolveServiceInArchimedes(hostPort)
			if err != nil {
				return false
			}
			return true
		})

		if err != nil {
			break
		}

		<-cacheTicker.C
	}

	close(c.refreshingChan)
}

func (c *Client) Do(req *Request) (*Response, error) {
	log.Debug("host in Do: ", req.Host)

	select {
	case <-c.refreshingChan:
		go c.refreshCachePeriodically()
	default:
	}

	reqId := uuid.New().String()
	c.beforeMiddlewares.Range(func(key, value interface{}) bool {
		midId := key.(middlewaresMapKey)
		midFunc := key.(middlewaresMapValue)
		log.Debugf("calling before middleware %s", midId)
		go midFunc(reqId, req)
		return true
	})

	hostPort := req.Host

	var (
		resolvedHostPort string
		usingCache, ok   bool
		err              error
	)

	value, ok := c.cache.Load(hostPort)
	if ok {
		resolvedHostPort = value.(AddressCacheValue)
		log.Debugf("resolved %s to %s using cache", hostPort, resolvedHostPort)
		usingCache = true
	} else {
		resolvedHostPort, err = c.resolveServiceInArchimedes(hostPort)
		if err != nil {
			panic(err)
		}
		log.Debugf("resolved %s to %s in archimedes", hostPort, resolvedHostPort)
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

	c.afterMiddlewares.Range(func(key, value interface{}) bool {
		midId := key.(middlewaresMapKey)
		midFunc := key.(middlewaresMapValue)
		log.Debugf("calling after middleware %s", midId)
		go midFunc(reqId, req)
		return true
	})

	resp, err := c.Client.Do(req)
	switch err.(type) {
	case net.Error:
		if usingCache && (err.(net.Error).Timeout() || strings.Contains(err.Error(), "no route to host")) {
			log.Debugf("got timeout using cached addr %s, will refresh cache entry", resolvedHostPort)
			c.cache.Delete(hostPort)
			resolvedHostPort, err = c.resolveServiceInArchimedes(hostPort)
			if err != nil {
				panic(err)
			}
			newUrl.Host = resolvedHostPort
			req, err = originalHttp.NewRequest(req.Method, newUrl.String(), req.Body)
			if err != nil {
				panic(err)
			}

			resp, err = c.Client.Do(req)
		}
	}

	return resp, err
}

// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
// WARN this is not really thread safe for now
func (c *Client) resolveServiceInArchimedes(hostPort string) (resolvedHostPort string, err error) {
	if c.archimedesClient == nil {
		c.archimedesClient = archimedes.NewArchimedesClient(archimedes.ServiceName)
	}

	host, rawPort, err := net.SplitHostPort(hostPort)
	if err != nil {
		log.Error("hostport: ", hostPort)
		panic(err)
	}

	port := nat.Port(rawPort + "/tcp")
	rHost, rPort, status := c.archimedesClient.Resolve(host, port)
	switch status {
	case StatusSeeOther:

	case StatusNotFound:
		log.Debugf("could not resolve %s", hostPort)
		return hostPort, nil
	case StatusOK:
	default:
		return "", errors.New(
			fmt.Sprintf("got status %d while resolving %s in archimedes", status, hostPort))
	}

	resolvedHostPort = rHost + ":" + rPort
	c.cache.Store(hostPort, resolvedHostPort)

	return resolvedHostPort, err
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
