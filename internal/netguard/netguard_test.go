package netguard_test

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/askuksov/yalkos/internal/netguard"
)

// Ranges blocked for every destination, and ranges blocked only when private
// networks are not allowed.
var (
	alwaysBlocked = map[string]string{
		"0.0.0.0/8":          "0.1.2.3",
		"169.254.0.0/16":     "169.254.169.254",
		"fe80::/10":          "fe80::1",
		"::/128":             "::",
		"224.0.0.0/4":        "239.255.255.250",
		"ff00::/8":           "ff02::1",
		"255.255.255.255/32": "255.255.255.255",
	}
	privateBlocked = map[string]string{
		"127.0.0.0/8":    "127.0.0.1",
		"::1/128":        "::1",
		"10.0.0.0/8":     "10.1.2.3",
		"172.16.0.0/12":  "172.31.255.254",
		"192.168.0.0/16": "192.168.1.1",
		"fc00::/7":       "fd12:3456::1",
		"100.64.0.0/10":  "100.100.100.100",
	}
)

func TestCheckRanges(t *testing.T) {
	for cidr, ip := range alwaysBlocked {
		for _, allowPrivate := range []bool{true, false} {
			err := netguard.Check(netip.MustParseAddr(ip), allowPrivate)
			if !errors.Is(err, netguard.ErrBlocked) {
				t.Errorf("%s (%s) allowPrivate=%v: err = %v, want ErrBlocked", ip, cidr, allowPrivate, err)
			}
		}
	}
	for cidr, ip := range privateBlocked {
		if err := netguard.Check(netip.MustParseAddr(ip), true); err != nil {
			t.Errorf("%s (%s) allowPrivate=true: err = %v, want nil", ip, cidr, err)
		}
		if err := netguard.Check(netip.MustParseAddr(ip), false); !errors.Is(err, netguard.ErrBlocked) {
			t.Errorf("%s (%s) allowPrivate=false: err = %v, want ErrBlocked", ip, cidr, err)
		}
	}

	// Positive controls: public addresses pass in both modes.
	for _, ip := range []string{"8.8.8.8", "2001:4860::8888", "1.1.1.1"} {
		for _, allowPrivate := range []bool{true, false} {
			if err := netguard.Check(netip.MustParseAddr(ip), allowPrivate); err != nil {
				t.Errorf("%s allowPrivate=%v: err = %v, want nil", ip, allowPrivate, err)
			}
		}
	}

	// One step outside the link-local range.
	if err := netguard.Check(netip.MustParseAddr("169.254.0.0"), true); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("169.254.0.0: err = %v, want ErrBlocked", err)
	}
	if err := netguard.Check(netip.MustParseAddr("169.255.0.1"), true); err != nil {
		t.Errorf("169.255.0.1: err = %v, want nil", err)
	}
}

func TestCheckUnmapsIPv4Mapped(t *testing.T) {
	err := netguard.Check(netip.MustParseAddr("::ffff:169.254.169.254"), true)
	if !errors.Is(err, netguard.ErrBlocked) {
		t.Fatalf("mapped metadata address: err = %v, want ErrBlocked", err)
	}
	if want := "address not allowed: 169.254.169.254"; err.Error() != want {
		t.Errorf("message = %q, want %q", err.Error(), want)
	}
	// Mapped private address follows the private switch.
	mapped := netip.MustParseAddr("::ffff:10.0.0.1")
	if err := netguard.Check(mapped, true); err != nil {
		t.Errorf("mapped private, allowed: %v", err)
	}
	if err := netguard.Check(mapped, false); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("mapped private, not allowed: err = %v, want ErrBlocked", err)
	}
	// Positive control: a mapped public address passes.
	if err := netguard.Check(netip.MustParseAddr("::ffff:8.8.8.8"), false); err != nil {
		t.Errorf("mapped public: %v", err)
	}
}

func TestCheckLinkLocalWithZone(t *testing.T) {
	// A zone must not slip an address past the prefix match.
	if err := netguard.Check(netip.MustParseAddr("fe80::1%eth0"), true); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("zoned link-local: err = %v, want ErrBlocked", err)
	}
	if err := netguard.Check(netip.Addr{}, true); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("invalid address: err = %v, want ErrBlocked", err)
	}
}

func TestControlRejectsBlockedAddress(t *testing.T) {
	control := netguard.Control(false)
	if err := control("tcp", "127.0.0.1:80", nil); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("127.0.0.1:80: err = %v, want ErrBlocked", err)
	}
	if err := control("tcp6", "[fe80::1]:80", nil); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("[fe80::1]:80: err = %v, want ErrBlocked", err)
	}
	if err := control("tcp", "not an address", nil); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("unparsable address: err = %v, want ErrBlocked", err)
	}
	if err := control("tcp", "93.184.216.34:80", nil); err != nil {
		t.Errorf("93.184.216.34:80: err = %v, want nil", err)
	}
	// The private switch is honoured.
	if err := netguard.Control(true)("tcp", "127.0.0.1:80", nil); err != nil {
		t.Errorf("allowPrivate=true 127.0.0.1:80: err = %v, want nil", err)
	}
	if err := netguard.Control(true)("tcp", "169.254.169.254:80", nil); !errors.Is(err, netguard.ErrBlocked) {
		t.Errorf("allowPrivate=true 169.254.169.254:80: err = %v, want ErrBlocked", err)
	}
}
