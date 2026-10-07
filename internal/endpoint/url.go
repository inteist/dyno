// Package endpoint validates compatible API bases without exposing credentials.
package endpoint

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const DefaultBaseURL = "http://127.0.0.1:8000/v1"

// Parse validates and canonicalizes an API base. Host case and default ports
// are normalized; escaped path semantics are preserved. Diagnostics omit input.
func Parse(raw string) (*url.URL, error) {
	invalid := errors.New("API base must be an absolute http(s) URL without credentials, query, fragment or controls, with a port from 1 to 65535")
	if strings.IndexFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return nil, invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return nil, invalid
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || u.Opaque != "" {
		return nil, invalid
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		if !strings.HasPrefix(u.Host, "[") {
			return nil, invalid
		}
		if _, err := netip.ParseAddr(host); err != nil {
			return nil, invalid
		}
	} else if strings.ContainsAny(host, "[]\\") {
		return nil, invalid
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		return nil, invalid
	}
	if port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return nil, invalid
		}
		port = strconv.FormatUint(number, 10)
		if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
			port = ""
		}
	}
	u.Host = host
	if port != "" {
		u.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	}
	if strings.HasSuffix(u.EscapedPath(), "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
		u.RawPath = strings.TrimSuffix(u.RawPath, "/")
	}
	if strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return nil, invalid
	}
	return u, nil
}

func IsLoopback(u *url.URL) bool {
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(u.Hostname())
	return err == nil && ip.Unmap().IsLoopback()
}
