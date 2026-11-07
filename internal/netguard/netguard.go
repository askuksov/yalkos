// Package netguard decides which network addresses a webhook may be sent to.
// The check runs on the address a connection actually dials, so a host name
// that resolves to a forbidden address (or changes its answer between checks)
// cannot get around it.
package netguard

import (
	"errors"
	"fmt"
	"net/netip"
	"syscall"
)

// ErrBlocked is wrapped by every error for a forbidden address.
var ErrBlocked = errors.New("address not allowed")

func prefixes(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

var (
	// alwaysBlocked: unspecified, link-local (cloud metadata), multicast and broadcast.
	alwaysBlocked = prefixes(
		"0.0.0.0/8", "169.254.0.0/16", "fe80::/10", "::/128",
		"224.0.0.0/4", "ff00::/8", "255.255.255.255/32",
	)
	// privateBlocked: loopback, private and shared address space.
	privateBlocked = prefixes(
		"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12",
		"192.168.0.0/16", "fc00::/7", "100.64.0.0/10",
	)
)

func inAny(ip netip.Addr, list []netip.Prefix) bool {
	for _, p := range list {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Check reports whether ip may be dialed. Private and loopback addresses are
// refused unless allowPrivate; the always-blocked ranges are refused either
// way. The error wraps ErrBlocked.
func Check(ip netip.Addr, allowPrivate bool) error {
	// Prefix.Contains is false for zoned addresses and IPv4-mapped IPv6 is
	// matched against IPv6 ranges only, so normalise first.
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || inAny(ip, alwaysBlocked) || (!allowPrivate && inAny(ip, privateBlocked)) {
		return fmt.Errorf("%w: %s", ErrBlocked, ip)
	}
	return nil
}

// Control returns a net.Dialer.Control hook that applies Check to the address
// being dialed; an address that cannot be parsed is refused.
func Control(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrBlocked, address)
		}
		return Check(ap.Addr(), allowPrivate)
	}
}
