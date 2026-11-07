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
	// alwaysBlocked: unspecified, link-local, multicast and broadcast, the
	// cloud metadata addresses that sit inside private ranges (AWS over IPv6,
	// Alibaba), and IPv6 ranges that tunnel to arbitrary IPv4 hosts (Teredo)
	// or are reserved for local use (NAT64 local-use prefix).
	alwaysBlocked = prefixes(
		"0.0.0.0/8", "169.254.0.0/16", "fe80::/10", "::/128",
		"224.0.0.0/4", "ff00::/8", "255.255.255.255/32",
		"fd00:ec2::254/128", "100.100.100.200/32",
		"2001::/32", "64:ff9b:1::/48",
	)
	// privateBlocked: loopback, private and shared address space.
	privateBlocked = prefixes(
		"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12",
		"192.168.0.0/16", "fc00::/7", "100.64.0.0/10",
	)
	// Prefixes that carry an IPv4 address: NAT64 and IPv4-compatible hold it
	// in the last 32 bits, 6to4 in bits 16 to 47.
	nat64        = netip.MustParsePrefix("64:ff9b::/96")
	v4Compatible = netip.MustParsePrefix("::/96")
	sixToFour    = netip.MustParsePrefix("2002::/16")
	loopback6    = netip.MustParseAddr("::1")
)

func inAny(ip netip.Addr, list []netip.Prefix) bool {
	for _, p := range list {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// embeddedIPv4 returns the IPv4 address carried inside a NAT64, IPv4-compatible
// or 6to4 address. "::" and "::1" are not IPv4-compatible addresses; they keep
// their own rules.
func embeddedIPv4(ip netip.Addr) (netip.Addr, bool) {
	b := ip.As16()
	switch {
	case nat64.Contains(ip):
		return netip.AddrFrom4([4]byte(b[12:])), true
	case v4Compatible.Contains(ip) && !ip.IsUnspecified() && ip != loopback6:
		return netip.AddrFrom4([4]byte(b[12:])), true
	case sixToFour.Contains(ip):
		return netip.AddrFrom4([4]byte(b[2:6])), true
	}
	return netip.Addr{}, false
}

// blocked applies the range rules to a normalised address.
func blocked(ip netip.Addr, allowPrivate bool) bool {
	if inAny(ip, alwaysBlocked) || (!allowPrivate && inAny(ip, privateBlocked)) {
		return true
	}
	if v4, ok := embeddedIPv4(ip); ok {
		return inAny(v4, alwaysBlocked) || (!allowPrivate && inAny(v4, privateBlocked))
	}
	return false
}

// Check reports whether ip may be dialed. Private and loopback addresses are
// refused unless allowPrivate; the always-blocked ranges are refused either
// way. An IPv4 address embedded in an IPv6 address (IPv4-mapped, NAT64,
// IPv4-compatible, 6to4) is judged by the same rules as the IPv4 address
// itself. The error wraps ErrBlocked.
func Check(ip netip.Addr, allowPrivate bool) error {
	// Prefix.Contains is false for zoned addresses and IPv4-mapped IPv6 is
	// matched against IPv6 ranges only, so normalise first.
	ip = ip.Unmap().WithZone("")
	if !ip.IsValid() || blocked(ip, allowPrivate) {
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
