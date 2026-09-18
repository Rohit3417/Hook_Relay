package ssrf

/* even if you resolve the hostname once and check that IP,
an attacker can configure their DNS to return a safe IP the first time (when you check),
then a different, dangerous IP a moment later (when you actually connect).
If your check and your connection are two separate steps using two separate DNS lookups,
there's a window where the attacker can swap the answer.
*/

import (
	"log"
	"net"
)

// building the blocked list every time is not necessary so write a function only once it is called
var blockedNetworks = mustParseBlockedNetworks()

func mustParseBlockedNetworks() []net.IPNet {
	var blockedRanges = []string{ // This is the starting point of range of address we need to block
		"127.0.0.0/8",    // loopback
		"10.0.0.0/8",     // private
		"172.16.0.0/12",  // private
		"192.168.0.0/16", // private
		"169.254.0.0/16", // link-local, includes cloud metadata 169.254.169.254
		"::1/128",        // IPv6 loopback
	}

	var networks []net.IPNet

	for _, cidr := range blockedRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Fatalf("ssrf: invalid CIDR %q: %v", cidr, err)
		}

		networks = append(networks, *network)
	}

	return networks
}

func IsBlockedIP(ip net.IP) bool {

	for _, network := range blockedNetworks {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}
