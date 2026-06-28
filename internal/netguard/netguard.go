// Package netguard contains the outbound network safety checks used by both the
// API validation path and the worker dial path. Keeping both layers here avoids
// accepting a URL at submission time that later bypasses policy through DNS.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// ErrBlockedTarget marks destinations that must never be used for vendor
// callbacks because they point at local infrastructure rather than the public
// internet.
var ErrBlockedTarget = errors.New("target resolves to a private or internal address")

// RejectBlockedHost handles cheap checks that do not require DNS. It is used at
// request validation time so obviously unsafe targets fail before being queued.
func RejectBlockedHost(host string) error {
	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if normalized == "" {
		return fmt.Errorf("%w: empty host", ErrBlockedTarget)
	}
	if normalized == "localhost" || strings.HasSuffix(normalized, ".localhost") {
		return fmt.Errorf("%w: localhost is not allowed", ErrBlockedTarget)
	}
	if addr, err := netip.ParseAddr(normalized); err == nil {
		return RejectBlockedAddr(addr)
	}
	return nil
}

// ResolvePublicAddress resolves the final dial target and rejects every returned
// address if any answer points at an internal range. This second check is needed
// because a public-looking hostname can resolve differently after submission.
func ResolvePublicAddress(ctx context.Context, address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if err := RejectBlockedHost(host); err != nil {
		return "", err
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		return net.JoinHostPort(addr.String(), port), nil
	}

	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("no addresses resolved for %s", host)
	}
	for _, addr := range addrs {
		if err := RejectBlockedAddr(addr); err != nil {
			return "", err
		}
	}
	return net.JoinHostPort(addrs[0].String(), port), nil
}

// RejectBlockedAddr enforces the SSRF boundary for concrete IP addresses.
func RejectBlockedAddr(addr netip.Addr) error {
	if addr.Is4In6() {
		addr = netip.AddrFrom4(addr.As4())
	}
	if addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		inPrefix(addr, "100.64.0.0/10") ||
		inPrefix(addr, "0.0.0.0/8") {
		return fmt.Errorf("%w: %s", ErrBlockedTarget, addr)
	}
	return nil
}

func inPrefix(addr netip.Addr, prefix string) bool {
	parsed := netip.MustParsePrefix(prefix)
	return parsed.Contains(addr)
}
