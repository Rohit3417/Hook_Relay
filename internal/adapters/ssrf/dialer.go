package ssrf

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

func MakeCall(network, address string, c syscall.RawConn) error {
	//address arrives as "ip:port"
	//So we split
	ip, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parsing address %q: %w", address, err)
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return fmt.Errorf("could not parse IP from address %q", address)

	}
	if IsBlockedIP(parsedIP) {
		return fmt.Errorf("blocked network")
	}

	return nil
}

func NewSecureClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: MakeCall,
	}

	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}

	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	return client
}
