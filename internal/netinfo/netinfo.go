// Package netinfo finds the addresses other devices can use to reach Printty:
// this computer's LAN IPs (Wi-Fi, Ethernet), a Tailscale IP, and its name.
package netinfo

import (
	"net"
	"os"
	"sort"
	"strings"
)

// Address is one way to reach Printty.
type Address struct {
	URL   string `json:"url"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // lan, tailscale, name
}

var skipIface = []string{"vethernet", "virtualbox", "vmware", "hyper-v", "wsl", "docker", "loopback", "bluetooth", "vpn", "npcap", "zerotier"}

// URLs lists the addresses for a server listening on addr ("" host = all interfaces).
func URLs(addr string, secure bool) []Address {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	scheme := "http"
	if secure {
		scheme = "https"
	}
	mk := func(h string) string { return scheme + "://" + net.JoinHostPort(h, port) }

	// Bound to one address: that's the only way in.
	if host != "" && host != "0.0.0.0" && host != "::" {
		ip := net.ParseIP(host)
		if ip != nil && ip.IsLoopback() {
			return []Address{{URL: mk(host), Label: "This computer only", Kind: "local"}}
		}
		return []Address{{URL: mk(host), Label: "Network", Kind: "lan"}}
	}

	var out []Address
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		tailscale := strings.Contains(name, "tailscale")
		skip := false
		for _, s := range skipIface {
			if strings.Contains(name, s) {
				skip = true
			}
		}
		if skip && !tailscale {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() {
				continue
			}
			switch {
			case isCGNAT(ip) && tailscale:
				out = append(out, Address{URL: mk(ip.String()), Label: "Tailscale (works away from home)", Kind: "tailscale"})
			case ip.IsPrivate() && !tailscale:
				out = append(out, Address{URL: mk(ip.String()), Label: friendly(ifc.Name), Kind: "lan"})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	if h, err := os.Hostname(); err == nil && h != "" && len(out) > 0 {
		out = append(out, Address{URL: mk(strings.ToLower(h) + ".local"), Label: "This PC's name (newer phones)", Kind: "name"})
	}
	return out
}

func isCGNAT(ip net.IP) bool { return ip[0] == 100 && ip[1]&0xC0 == 64 } // 100.64.0.0/10

func rank(a Address) int {
	switch {
	case a.Kind == "lan" && strings.Contains(strings.ToLower(a.Label), "wi-fi"):
		return 0
	case a.Kind == "lan":
		return 1
	case a.Kind == "tailscale":
		return 2
	}
	return 3
}

func friendly(iface string) string {
	n := strings.ToLower(iface)
	switch {
	case strings.Contains(n, "wi-fi"), strings.Contains(n, "wifi"), strings.Contains(n, "wlan"), strings.Contains(n, "wireless"):
		return "Wi-Fi"
	case strings.Contains(n, "ethernet"), strings.HasPrefix(n, "eth"), strings.HasPrefix(n, "en"):
		return "Ethernet"
	}
	return iface
}

// Primary returns the URL most likely to work for phones on the same network.
func Primary(list []Address) string {
	for _, a := range list {
		if a.Kind == "lan" {
			return a.URL
		}
	}
	if len(list) > 0 {
		return list[0].URL
	}
	return ""
}
