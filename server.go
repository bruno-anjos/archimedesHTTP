// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP server. See RFC 7230 through 7235.

package http

import (
	"net"
	originalHttp "net/http"
	"time"

	"github.com/bruno-anjos/cloud-edge-deployment/pkg/deployer"
	"github.com/bruno-anjos/cloud-edge-deployment/pkg/utils"
)

const (
	DefaultMaxHeaderBytes = 1 << 20 // 1 MB

	TimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"

	TrailerPrefix = "Trailer:"

	StateNew      = originalHttp.StateNew
	StateActive   = originalHttp.StateActive
	StateIdle     = originalHttp.StateIdle
	StateHijacked = originalHttp.StateHijacked
	StateClosed   = originalHttp.StateClosed
)

// A ResponseWriter interface is used by an HTTP handler to
// construct an HTTP response.
//
// A ResponseWriter may not be used after the Handler.ServeHTTP method
// has returned.
type (
	Handler        = originalHttp.Handler
	ResponseWriter = originalHttp.ResponseWriter
	Flusher        = originalHttp.Flusher
	Hijacker       = originalHttp.Hijacker
	CloseNotifier  = originalHttp.CloseNotifier

	HandlerFunc = originalHttp.HandlerFunc
	ServeMux    = originalHttp.ServeMux
)

var (
	ErrBodyNotAllowed  = originalHttp.ErrBodyNotAllowed
	ErrHijacked        = originalHttp.ErrHijacked
	ErrContentLength   = originalHttp.ErrContentLength
	ErrWriteAfterFlush = originalHttp.ErrWriteAfterFlush

	DefaultServeMux = originalHttp.DefaultServeMux

	ServerContextKey    = originalHttp.ServerContextKey
	LocalAddrContextKey = originalHttp.LocalAddrContextKey

	ErrAbortHandler = originalHttp.ErrAbortHandler

	ErrServerClosed = originalHttp.ErrServerClosed

	ErrHandlerTimeout = originalHttp.ErrHandlerTimeout
)

// Serve accepts incoming HTTP connections on the listener l,
// creating a new service goroutine for each. The service goroutines
// read requests and then call handler to reply to them.
//
// The handler is typically nil, in which case the DefaultServeMux is used.
//
// HTTP/2 support is only enabled if the Listener returns *tls.Conn
// connections and they were configured with "h2" in the TLS
// Config.NextProtos.
//
// Serve always returns a non-nil error.
func Serve(l net.Listener, handler originalHttp.Handler) error {
	srv := &originalHttp.Server{Handler: handler}
	go deployer.NewDeployerClient(utils.DeployerServiceName).SendInstanceHeartbeatToDeployerPeriodically()
	return srv.Serve(l)
}

// ServeTLS accepts incoming HTTPS connections on the listener l,
// creating a new service goroutine for each. The service goroutines
// read requests and then call handler to reply to them.
//
// The handler is typically nil, in which case the DefaultServeMux is used.
//
// Additionally, files containing a certificate and matching private key
// for the server must be provided. If the certificate is signed by a
// certificate authority, the certFile should be the concatenation
// of the server's certificate, any intermediates, and the CA's certificate.
//
// ServeTLS always returns a non-nil error.
func ServeTLS(l net.Listener, handler originalHttp.Handler, certFile, keyFile string) error {
	go deployer.NewDeployerClient(utils.DeployerServiceName).SendInstanceHeartbeatToDeployerPeriodically()
	srv := &originalHttp.Server{Handler: handler}
	return srv.ServeTLS(l, certFile, keyFile)
}

// ListenAndServe listens on the TCP network address addr and then calls
// Serve with handler to handle requests on incoming connections.
// Accepted connections are configured to enable TCP keep-alives.
//
// The handler is typically nil, in which case the DefaultServeMux is used.
//
// ListenAndServe always returns a non-nil error.
// TODO ARCHIMEDES HTTP CLIENT CHANGED THIS METHOD
func ListenAndServe(addr string, handler originalHttp.Handler) error {
	go deployer.NewDeployerClient(utils.DeployerServiceName).SendInstanceHeartbeatToDeployerPeriodically()
	server := &originalHttp.Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}

// ListenAndServeTLS acts identically to ListenAndServe, except that it
// expects HTTPS connections. Additionally, files containing a certificate and
// matching private key for the server must be provided. If the certificate
// is signed by a certificate authority, the certFile should be the concatenation
// of the server's certificate, any intermediates, and the CA's certificate.
func ListenAndServeTLS(addr, certFile, keyFile string, handler originalHttp.Handler) error {
	go deployer.NewDeployerClient(utils.DeployerServiceName).SendInstanceHeartbeatToDeployerPeriodically()
	server := &originalHttp.Server{Addr: addr, Handler: handler}
	return server.ListenAndServeTLS(certFile, keyFile)
}

func Error(w ResponseWriter, error string, code int) {
	originalHttp.Error(w, error, code)
}

func NotFound(w ResponseWriter, r *Request) { originalHttp.NotFound(w, r) }

func NotFoundHandler() Handler { return originalHttp.NotFoundHandler() }

func StripPrefix(prefix string, h Handler) Handler {
	return originalHttp.StripPrefix(prefix, h)
}

func Redirect(w ResponseWriter, r *Request, url string, code int) {
	originalHttp.Redirect(w, r, url, code)
}

func RedirectHandler(url string, code int) Handler {
	return originalHttp.RedirectHandler(url, code)
}

func NewServeMux() *ServeMux { return originalHttp.NewServeMux() }

func Handle(pattern string, handler Handler) { originalHttp.Handle(pattern, handler) }

// HandleFunc registers the handler function for the given pattern
// in the DefaultServeMux.
// The documentation for ServeMux explains how patterns are matched.
func HandleFunc(pattern string, handler func(ResponseWriter, *Request)) {
	HandleFunc(pattern, handler)
}

func TimeoutHandler(h Handler, dt time.Duration, msg string) Handler {
	return originalHttp.TimeoutHandler(h, dt, msg)
}
