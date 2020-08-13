// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// HTTP client. See RFC 7230 through 7235.
//
// This is the high-level Client interface.
// The low-level implementation is in transport.go.

package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	originalHttp "net/http"
	"net/url"
	"strings"

	archimedes "github.com/bruno-anjos/archimedes/api"
	genericutils "github.com/bruno-anjos/solution-utils"
	"github.com/docker/go-connections/nat"
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
	log.Debug("host in resolve: ", hostPort)
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		log.Error("hostPort: ", hostPort)
		panic(err)
	}

	archUrl := url.URL{
		Scheme: "http",
		Host:   archimedes.DefaultHostPort,
		Path:   archimedes.GetServicePath(host),
	}

	archReq, err := originalHttp.NewRequest(originalHttp.MethodGet, archUrl.String(), nil)
	if err != nil {
		panic(err)
	}

	resp, err := c.Client.Do(archReq)
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

	log.Debugf("got service %+v", service)

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
		Scheme: "http",
		Host:   archimedes.DefaultHostPort,
		Path:   archimedes.GetInstancePath(host),
	}

	archReq, err := originalHttp.NewRequest(originalHttp.MethodGet, archUrl.String(), nil)
	if err != nil {
		panic(err)
	}

	resp, err := c.Client.Do(archReq)
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

	log.Debugf("got instance %+v", completedInstance)

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
