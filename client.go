// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP client. See RFC 7230 through 7235.
//
// This is the high-level Client interface.
// The low-level implementation is in transport.go.

package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	originalHttp "net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	archimedes "github.com/bruno-anjos/archimedes/api"
	genericutils "github.com/bruno-anjos/solution-utils"
	"github.com/docker/go-connections/nat"
	log "github.com/sirupsen/logrus"
)

// A Client is an HTTP client. Its zero value (DefaultClient) is a
// usable client that uses DefaultTransport.
//
// The Client's Transport typically has internal state (cached TCP
// connections), so Clients should be reused instead of created as
// needed. Clients are safe for concurrent use by multiple goroutines.
//
// A Client is higher-level than a RoundTripper (such as Transport)
// and additionally handles HTTP details such as cookies and
// redirects.
//
// When following redirects, the Client will forward all headers set on the
// initial Request except:
//
// • when forwarding sensitive headers like "Authorization",
// "WWW-Authenticate", and "Cookie" to untrusted targets.
// These headers will be ignored when following a redirect to a domain
// that is not a subdomain match or exact match of the initial domain.
// For example, a redirect from "foo.com" to either "foo.com" or "sub.foo.com"
// will forward the sensitive headers, but a redirect to "bar.com" will not.
//
// • when forwarding the "Cookie" header with a non-nil cookie Jar.
// Since each redirect may mutate the state of the cookie jar,
// a redirect may possibly alter a cookie set in the initial request.
// When forwarding the "Cookie" header, any mutated cookies will be omitted,
// with the expectation that the Jar will insert those mutated cookies
// with the updated values (assuming the origin matches).
// If Jar is nil, the initial cookies are forwarded without change.
//
type Client struct {
	// Transport specifies the mechanism by which individual
	// HTTP requests are made.
	// If nil, DefaultTransport is used.
	Transport RoundTripper

	// CheckRedirect specifies the policy for handling redirects.
	// If CheckRedirect is not nil, the client calls it before
	// following an HTTP redirect. The arguments req and via are
	// the upcoming request and the requests made already, oldest
	// first. If CheckRedirect returns an error, the Client's Get
	// method returns both the previous Response (with its Body
	// closed) and CheckRedirect's error (wrapped in a url.Error)
	// instead of issuing the Request req.
	// As a special case, if CheckRedirect returns ErrUseLastResponse,
	// then the most recent response is returned with its body
	// unclosed, along with a nil error.
	//
	// If CheckRedirect is nil, the Client uses its default policy,
	// which is to stop after 10 consecutive requests.
	CheckRedirect func(req *originalHttp.Request, via []*originalHttp.Request) error

	// Jar specifies the cookie jar.
	//
	// The Jar is used to insert relevant cookies into every
	// outbound Request and is updated with the cookie values
	// of every inbound Response. The Jar is consulted for every
	// redirect that the Client follows.
	//
	// If Jar is nil, cookies are only sent if they are explicitly
	// set on the Request.
	Jar originalHttp.CookieJar

	// Timeout specifies a time limit for requests made by this
	// Client. The timeout includes connection time, any
	// redirects, and reading the response body. The timer remains
	// running after Get, Head, Post, or Do return and will
	// interrupt reading of the Response.Body.
	//
	// A Timeout of zero means no timeout.
	//
	// The Client cancels requests to the underlying Transport
	// as if the Request's Context ended.
	//
	// For compatibility, the Client will also use the deprecated
	// CancelRequest method on Transport if found. New
	// RoundTripper implementations should use the Request's Context
	// for cancellation instead of implementing CancelRequest.
	Timeout time.Duration

	originalHttpClient originalHttp.Client
}

// DefaultClient is the default Client and is used by Get, Head, and Post.
var DefaultClient = &Client{}

// RoundTripper is an interface representing the ability to execute a
// single HTTP transaction, obtaining the Response for a given Request.
//
// A RoundTripper must be safe for concurrent use by multiple
// goroutines.
type RoundTripper interface {
	// RoundTrip executes a single HTTP transaction, returning
	// a Response for the provided Request.
	//
	// RoundTrip should not attempt to interpret the response. In
	// particular, RoundTrip must return err == nil if it obtained
	// a response, regardless of the response's HTTP status code.
	// A non-nil err should be reserved for failure to obtain a
	// response. Similarly, RoundTrip should not attempt to
	// handle higher-level protocol details such as redirects,
	// authentication, or cookies.
	//
	// RoundTrip should not modify the request, except for
	// consuming and closing the Request's Body. RoundTrip may
	// read fields of the request in a separate goroutine. Callers
	// should not mutate or reuse the request until the Response's
	// Body has been closed.
	//
	// RoundTrip must always close the body, including on errors,
	// but depending on the implementation may do so in a separate
	// goroutine even after RoundTrip returns. This means that
	// callers wanting to reuse the body for subsequent requests
	// must arrange to wait for the Close call before doing so.
	//
	// The Request's URL and Header fields must be initialized.
	RoundTrip(*originalHttp.Request) (*originalHttp.Response, error)
}

// See 2 (end of page 4) https://www.ietf.org/rfc/rfc2617.txt
// "To receive authorization, the client sends the userid and password,
// separated by a single colon (":") character, within a base64
// encoded string in the credentials."
// It is not meant to be urlencoded.
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// Get issues a GET to the specified URL. If the response is one of
// the following redirect codes, Get follows the redirect, up to a
// maximum of 10 redirects:
//
//    301 (Moved Permanently)
//    302 (Found)
//    303 (See Other)
//    307 (Temporary Redirect)
//    308 (Permanent Redirect)
//
// An error is returned if there were too many redirects or if there
// was an HTTP protocol error. A non-2xx response doesn't cause an
// error. Any returned error will be of type *url.Error. The url.Error
// value's Timeout method will report true if request timed out or was
// canceled.
//
// When err is nil, resp always contains a non-nil resp.Body.
// Caller should close resp.Body when done reading from it.
//
// Get is a wrapper around DefaultClient.Get.
//
// To make a request with custom headers, use NewRequest and
// DefaultClient.Do.
func Get(url string) (resp *Response, err error) {
	return DefaultClient.Get(url)
}

// Get issues a GET to the specified URL. If the response is one of the
// following redirect codes, Get follows the redirect after calling the
// Client's CheckRedirect function:
//
//    301 (Moved Permanently)
//    302 (Found)
//    303 (See Other)
//    307 (Temporary Redirect)
//    308 (Permanent Redirect)
//
// An error is returned if the Client's CheckRedirect function fails
// or if there was an HTTP protocol error. A non-2xx response doesn't
// cause an error. Any returned error will be of type *url.Error. The
// url.Error value's Timeout method will report true if request timed
// out or was canceled.
//
// When err is nil, resp always contains a non-nil resp.Body.
// Caller should close resp.Body when done reading from it.
//
// To make a request with custom headers, use NewRequest and Client.Do.
func (c *Client) Get(url string) (resp *Response, err error) {
	req, err := NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// Do sends an HTTP request and returns an HTTP response, following
// policy (such as redirects, cookies, auth) as configured on the
// client.
//
// An error is returned if caused by client policy (such as
// CheckRedirect), or failure to speak HTTP (such as a network
// connectivity problem). A non-2xx status code doesn't cause an
// error.
//
// If the returned error is nil, the Response will contain a non-nil
// Body which the user is expected to close. If the Body is not both
// read to EOF and closed, the Client's underlying RoundTripper
// (typically Transport) may not be able to re-use a persistent TCP
// connection to the server for a subsequent "keep-alive" request.
//
// The request Body, if non-nil, will be closed by the underlying
// Transport, even on errors.
//
// On error, any Response can be ignored. A non-nil Response with a
// non-nil error only occurs when CheckRedirect fails, and even then
// the returned Response.Body is already closed.
//
// Generally Get, Post, or PostForm will be used instead of Do.
//
// If the server replies with a redirect, the Client first uses the
// CheckRedirect function to determine whether the redirect should be
// followed. If permitted, a 301, 302, or 303 redirect causes
// subsequent requests to use HTTP method GET
// (or HEAD if the original request was HEAD), with no body.
// A 307 or 308 redirect preserves the original HTTP method and body,
// provided that the Request.GetBody function is defined.
// The NewRequest function automatically sets GetBody for common
// standard library body types.
//
// Any returned error will be of type *url.Error. The url.Error
// value's Timeout method will report true if request timed out or was
// canceled.
// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
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

	req, err = NewRequest(req.Method, newUrl.String(), req.Body)
	if err != nil {
		panic(err)
	}

	resp, err := c.originalHttpClient.Do(req.ToOriginalRequest())

	return FromOriginalToCustomResponse(resp), err
}

// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
func (c *Client) resolveServiceInArchimedes(hostPort string) (resolvedHostPort string, err error) {
	log.Debug("host in resolve: ", hostPort)
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		log.Error("hostPort: ", hostPort)
		panic(err)
	}

	archUrl := url.URL{
		Scheme:     "http",
		Host:       archimedes.DefaultHostPort,
		Path:       archimedes.GetServicePath(host),
	}

	archReq, err := originalHttp.NewRequest(originalHttp.MethodGet, archUrl.String(), nil)
	if err != nil {
		panic(err)
	}

	resp, err := c.originalHttpClient.Do(archReq)
	if err != nil {
		panic(err)
	}

	if resp.StatusCode == originalHttp.StatusNotFound {
		log.Debugf("could not resolve service %s", hostPort)
		resolvedHostPort, err = c.resolveInstanceInArchimedes(hostPort)
		if err != nil {
			return "", err
		}
		return resolvedHostPort, nil
	} else if resp.StatusCode != originalHttp.StatusOK {
		return "", errors.New(
			fmt.Sprintf("got status %d while resolving %s in archimedes", resp.StatusCode, hostPort))
	}

	var service archimedes.CompletedServiceDTO
	err = json.NewDecoder(resp.Body).Decode(&service)
	if err != nil {
		panic(err)
	}

	portWithProto, err := nat.NewPort(genericutils.TCP, port)
	if err != nil {
		panic(err)
	}

	_, ok := service.Ports[portWithProto]
	if !ok {
		return "", errors.New(fmt.Sprintf("port is not valid for service %s", host))
	}

	randInstanceId := service.InstancesIds[rand.Intn(len(service.InstancesIds))]
	instance := service.InstancesMap[randInstanceId]

	var portResolved string
	if instance.Local {
		portResolved = portWithProto.Port()
	} else {
		portResolved = instance.PortTranslation[portWithProto][0].HostPort
	}

	resolvedHostPort = instance.Ip + ":" + portResolved

	log.Debugf("resolved %s to %s", hostPort, resolvedHostPort)

	return resolvedHostPort, nil
}

// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
func (c *Client) resolveInstanceInArchimedes(hostPort string) (resolvedHostPort string, err error) {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		panic(err)
	}

	archUrl := url.URL{
		Scheme:     "http",
		Host:       archimedes.DefaultHostPort,
		Path:       archimedes.GetInstancePath(host),
	}

	archReq, err := originalHttp.NewRequest(originalHttp.MethodGet, archUrl.String(),nil)
	if err != nil {
		panic(err)
	}

	resp, err := c.originalHttpClient.Do(archReq)
	if err != nil {
		panic(err)
	}

	if resp.StatusCode == originalHttp.StatusNotFound {
		log.Debugf("could not resolve instance %s", hostPort)
		return hostPort, nil
	} else if resp.StatusCode != originalHttp.StatusOK {
		return "", errors.New(
			fmt.Sprintf("got status %d while resolving %s in archimedes", resp.StatusCode, hostPort))
	}

	var completedInstance archimedes.CompletedInstanceDTO
	err = json.NewDecoder(resp.Body).Decode(&completedInstance)
	if err != nil {
		panic(err)
	}

	portWithProto, err := nat.NewPort(genericutils.TCP, port)
	if err != nil {
		panic(err)
	}

	_, ok := completedInstance.Ports[portWithProto]
	if !ok {
		return "", errors.New(fmt.Sprintf("port is not valid for service %s", host))
	}

	var portResolved string
	if completedInstance.Instance.Local {
		portResolved = portWithProto.Port()
	} else {
		portResolved = completedInstance.Instance.PortTranslation[portWithProto][0].HostPort
	}

	resolvedHostPort = completedInstance.Instance.Ip + ":" + portResolved

	log.Debugf("resolved %s to %s", hostPort, resolvedHostPort)

	return resolvedHostPort, nil
}

// makeHeadersCopier makes a function that copies headers from the
// initial Request, ireq. For every redirect, this function must be called
// so that it can copy headers into the upcoming Request.
func (c *Client) makeHeadersCopier(ireq *originalHttp.Request) func(*originalHttp.Request) {
	// The headers to copy are from the very initial request.
	// We use a closured callback to keep a reference to these original headers.
	var (
		ireqhdr  = cloneOrMakeHeader(ireq.Header)
		icookies map[string][]*Cookie
	)
	if c.Jar != nil && ireq.Header.Get("Cookie") != "" {
		icookies = make(map[string][]*Cookie)
		for _, c := range ireq.Cookies() {
			icookies[c.Name] = append(icookies[c.Name], FromOriginalToCustomCookie(c))
		}
	}

	preq := ireq // The previous request
	return func(req *originalHttp.Request) {
		// If Jar is present and there was some initial cookies provided
		// via the request header, then we may need to alter the initial
		// cookies as we follow redirects since each redirect may end up
		// modifying a pre-existing cookie.
		//
		// Since cookies already set in the request header do not contain
		// information about the original domain and path, the logic below
		// assumes any new set cookies override the original cookie
		// regardless of domain or path.
		//
		// See https://golang.org/issue/17494
		if c.Jar != nil && icookies != nil {
			var changed bool
			resp := req.Response // The response that caused the upcoming redirect
			for _, c := range resp.Cookies() {
				if _, ok := icookies[c.Name]; ok {
					delete(icookies, c.Name)
					changed = true
				}
			}
			if changed {
				ireqhdr.Del("Cookie")
				var ss []string
				for _, cs := range icookies {
					for _, c := range cs {
						ss = append(ss, c.Name+"="+c.Value)
					}
				}
				sort.Strings(ss) // Ensure deterministic headers
				ireqhdr.Set("Cookie", strings.Join(ss, "; "))
			}
		}

		// Copy the initial request's Header values
		// (at least the safe ones).
		for k, vv := range ireqhdr {
			if shouldCopyHeaderOnRedirect(k, preq.URL, req.URL) {
				req.Header[k] = vv
			}
		}

		preq = req // Update previous Request with the current request
	}
}

func defaultCheckRedirect(_ *originalHttp.Request, via []*originalHttp.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
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
	req, err := NewRequest("POST", url, body)
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
	req, err := NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

func shouldCopyHeaderOnRedirect(headerKey string, initial, dest *url.URL) bool {
	switch CanonicalHeaderKey(headerKey) {
	case "Authorization", "Www-Authenticate", "Cookie", "Cookie2":
		// Permit sending auth/cookie headers from "foo.com"
		// to "sub.foo.com".

		// Note that we don't send all cookies to subdomains
		// automatically. This function is only used for
		// Cookies set explicitly on the initial outgoing
		// client request. Cookies automatically added via the
		// CookieJar mechanism continue to follow each
		// cookie's scope as set by Set-Cookie. But for
		// outgoing requests with the Cookie header set
		// directly, we don't know their scope, so we assume
		// it's for *.domain.com.

		ihost := canonicalAddr(initial)
		dhost := canonicalAddr(dest)
		return isDomainOrSubdomain(dhost, ihost)
	}
	// All other headers are copied:
	return true
}

// isDomainOrSubdomain reports whether sub is a subdomain (or exact
// match) of the parent domain.
//
// Both domains must already be in canonical form.
func isDomainOrSubdomain(sub, parent string) bool {
	if sub == parent {
		return true
	}
	// If sub is "foo.example.com" and parent is "example.com",
	// that means sub must end in "."+parent.
	// Do it without allocating.
	if !strings.HasSuffix(sub, parent) {
		return false
	}
	return sub[len(sub)-len(parent)-1] == '.'
}
