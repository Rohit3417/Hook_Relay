package ssrf

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Use of IsBlockedIp
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

// Creating a http client with the dialer rejecting blocked IP's
func NewSecureClient() *http.Client {

	// This timeout is for TCP connection
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: MakeCall,
	}

	//whenever you need to open a new connection, use this dialer instead of the default one"
	transport := &http.Transport{
		DialContext: dialer.DialContext,
	}

	//client.Timeout bounds the entire request — connection, sending the request, waiting for the response, reading the body, all of it combined.
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}

	return client
}
