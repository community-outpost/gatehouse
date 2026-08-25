package httpapi

import (
	"fmt"
	"net/http"
	"net/netip"
	"strings"
)

func trustedProxyClientIP(cidrs []string) (func(http.Handler) http.Handler, error) {
	trusted := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err != nil {
			return nil, fmt.Errorf("parse trusted proxy CIDR %q: %w", cidr, err)
		}
		trusted = append(trusted, prefix.Masked())
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			peer, ok := parsePeerIP(request.RemoteAddr)
			if !ok || !isTrustedProxy(peer, trusted) {
				next.ServeHTTP(writer, request)
				return
			}

			if client, ok := forwardedClientIP(request, trusted); ok {
				request.RemoteAddr = client.String()
			}
			next.ServeHTTP(writer, request)
		})
	}, nil
}

func forwardedClientIP(request *http.Request, trusted []netip.Prefix) (netip.Addr, bool) {
	if values := request.Header.Values("X-Forwarded-For"); len(values) > 0 {
		chain := strings.Split(strings.Join(values, ","), ",")
		for index := len(chain) - 1; index >= 0; index-- {
			address, err := netip.ParseAddr(strings.TrimSpace(chain[index]))
			if err != nil {
				return netip.Addr{}, false
			}
			address = address.Unmap()
			if !isTrustedProxy(address, trusted) {
				return address, true
			}
		}
		return netip.Addr{}, false
	}

	address, err := netip.ParseAddr(strings.TrimSpace(request.Header.Get("X-Real-Ip")))
	if err != nil {
		return netip.Addr{}, false
	}
	address = address.Unmap()
	if isTrustedProxy(address, trusted) {
		return netip.Addr{}, false
	}
	return address, true
}

func parsePeerIP(remoteAddress string) (netip.Addr, bool) {
	if addressPort, err := netip.ParseAddrPort(remoteAddress); err == nil {
		return addressPort.Addr().Unmap(), true
	}
	address, err := netip.ParseAddr(remoteAddress)
	if err != nil {
		return netip.Addr{}, false
	}
	return address.Unmap(), true
}

func isTrustedProxy(address netip.Addr, trusted []netip.Prefix) bool {
	for _, prefix := range trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
