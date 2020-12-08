// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP client. See RFC 7230 through 7235.
//
// This is the high-level Client interface.
// The low-level implementation is in transport.go.

package http

import (
	"errors"
	"fmt"
	"io"
	"net"
	originalHttp "net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bruno-anjos/cloud-edge-deployment/pkg/archimedes"
	"github.com/bruno-anjos/cloud-edge-deployment/pkg/archimedes/client"
	"github.com/docker/go-connections/nat"
	"github.com/golang/geo/s2"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type (
	cacheEntry struct {
		stale    bool
		resolved string
		sync.RWMutex
	}
	addressCacheKey   = string
	addressCacheValue = *cacheEntry
)

func newCacheEntry(resolved string) *cacheEntry {
	return &cacheEntry{
		stale:    false,
		resolved: resolved,
		RWMutex:  sync.RWMutex{},
	}
}

func (c *cacheEntry) isStale() bool {
	c.RLock()
	defer c.RUnlock()
	return c.stale
}

func (c *cacheEntry) setStale(stale bool) {
	c.Lock()
	defer c.Unlock()
	c.stale = stale
}

func (c *cacheEntry) getResolved() string {
	c.RLock()
	defer c.RUnlock()
	return c.resolved
}

const (
	CacheExpiringTime      = 1 * time.Minute
	refreshCacheTimeout    = 30 * time.Second
	ResetToFallbackTimeout = 2 * time.Minute
	FallbackEnvVar         = "FALLBACK_URL"

	DefaultArchimedesPort = 50000
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
	beforeMiddlewares sync.Map
	afterMiddlewares  sync.Map
	archimedesClient  *client.Client
	fallbackAddr      string
	location          s2.CellID
	initialized       bool
	sync.RWMutex
}

var ErrUseLastResponse = originalHttp.ErrUseLastResponse

var DefaultClient = &Client{}

type RoundTripper = originalHttp.RoundTripper

func Get(url string) (resp *Response, err error) {
	return DefaultClient.Get(url)
}

// InitArchimedesClient initializes the archimedes client with the starting archimedes server host, the port to the
// archimedes server and the location where the user is at the moment.
func (c *Client) InitArchimedesClient(host string, port int, location s2.LatLng) {
	hostPort := host + ":" + strconv.Itoa(port)
	log.Infof("Starting archimedes client with host %s", hostPort)

	c.Lock()
	c.archimedesClient = client.NewArchimedesClient(hostPort)
	c.location = s2.CellIDFromLatLng(location)
	c.initialized = true

	fallbackAddr, exists := os.LookupEnv(FallbackEnvVar)
	if !exists {
		log.Panicf("could not load env var %s", FallbackEnvVar)
	}

	c.fallbackAddr = fallbackAddr
	c.Unlock()
	go c.refreshCachePeriodically()
	go c.resetToFallbackPeriodically()
}

func (c *Client) SetLocation(location s2.LatLng) {
	c.Lock()
	defer c.Unlock()
	c.location = s2.CellIDFromLatLng(location)
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
	cacheTicker := time.NewTicker(refreshCacheTimeout)
	log.Debugf("setting up cache refreshing")

	for {
		<-cacheTicker.C

		log.Debugf("refreshing cache")

		staleEntries := map[string]interface{}{}
		c.cache.Range(func(key, value interface{}) bool {
			hostPort := key.(addressCacheKey)
			entry := value.(addressCacheValue)
			if entry.isStale() {
				log.Debugf("adding entry for %s as stale", hostPort)
				staleEntries[hostPort] = nil
			}
			return true
		})

		for hostPort := range staleEntries {
			c.cache.Delete(hostPort)
		}
	}
}

func (c *Client) resetToFallbackPeriodically() {
	fallbackTicker := time.NewTicker(ResetToFallbackTimeout)
	log.Info("setting up fallback reset")

	for {
		<-fallbackTicker.C

		log.Infof("resetting to fallback %s", c.fallbackAddr)
		c.Lock()
		c.archimedesClient.SetHostPort(c.fallbackAddr + ":" + strconv.Itoa(archimedes.Port))
		c.Unlock()
	}
}

const (
	maxTries = 5
)

func (c *Client) Do(req *Request) (*Response, error) {
	if !c.initialized {
		panic("client has not been initialized")
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
		found            bool
	)

	value, ok := c.cache.Load(hostPort)
	if ok {
		entry := value.(addressCacheValue)
		resolvedHostPort = entry.getResolved()
		log.Infof("resolved %s to %s using cache", hostPort, resolvedHostPort)
		usingCache = true
	} else {
		for i := 0; i < maxTries; i++ {
			resolvedHostPort, found, err = c.ResolveServiceInArchimedes(hostPort)
			if err != nil {
				panic(err)
			}

			if found {
				break
			}

			time.Sleep(time.Duration(1*(i+1)) * time.Second)
		}

		log.Infof("resolved %s to %s in archimedes", hostPort, resolvedHostPort)
	}

	oldUrl := req.URL
	newUrl := *oldUrl
	newUrl.Host = resolvedHostPort

	req.URL = &newUrl

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

			for i := 0; i < maxTries; i++ {
				resolvedHostPort, found, err = c.ResolveServiceInArchimedes(hostPort)
				if err != nil {
					panic(err)
				}

				if found {
					break
				}

				time.Sleep(time.Duration(1*(i+1)) * time.Second)
			}

			newUrl.Host = resolvedHostPort
			req.URL = &newUrl
			resp, err = c.Client.Do(req)
		}
	}

	return resp, err
}

// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
func (c *Client) ResolveServiceInArchimedes(hostPort string) (resolvedHostPort string, found bool, err error) {
	host, rawPort, err := net.SplitHostPort(hostPort)
	if err != nil {
		log.Error("hostport: ", hostPort)
		panic(err)
	}

	port := nat.Port(rawPort + "/tcp")

	deploymentId := strings.Split(host, "-")[0]
	start := time.Now()
	reqId, err := uuid.NewUUID()
	if err != nil {
		panic(err)
	}

	var (
		rHost, rPort string
		timedout     bool
		status       int
	)
	for {
		c.RLock()
		rHost, rPort, status, timedout = c.archimedesClient.Resolve(host, port, deploymentId, c.location, reqId.String())
		c.RUnlock()
		if !timedout {
			break
		} else {
			time.Sleep(2 * time.Second)
		}
	}

	switch status {
	case StatusSeeOther:

	case StatusNotFound:
		log.Debugf("could not resolve %s", hostPort)
		return hostPort, false, nil
	case StatusOK:
		log.Debugf("took %d to resolve %s", time.Since(start).Milliseconds(), reqId.String())
	default:
		return "", false,
			errors.New(fmt.Sprintf("got status %d while resolving %s in archimedes (req %s took %f)",
				status, hostPort, reqId.String(), time.Since(start).Seconds()))
	}

	resolvedHostPort = rHost + ":" + rPort
	entry := newCacheEntry(resolvedHostPort)
	c.cache.Store(hostPort, entry)
	go waitAndSetValueAsStale(entry)

	return resolvedHostPort, true, nil
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

func waitAndSetValueAsStale(entry *cacheEntry) {
	time.Sleep(CacheExpiringTime)
	entry.setStale(true)
}
