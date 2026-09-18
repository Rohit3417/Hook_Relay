package ssrf

import (
	"strings"
	"testing"
)

// func TestIsBlockedIP(t *testing.T) {
// 	tests := []struct {
// 		name     string
// 		ipStr    string
// 		expected bool
// 	}{
// 		// Blocked Networks (Should return true)
// 		{"IPv4 Loopback Localhost", "127.0.0.1", true},
// 		{"IPv4 Loopback Boundary", "127.255.255.254", true},
// 		{"IPv4 Private Class A", "10.0.0.1", true},
// 		{"IPv4 Private Class A High", "10.255.255.255", true},
// 		{"IPv4 Private Class B Low", "172.16.0.0", true},
// 		{"IPv4 Private Class B High", "172.31.255.255", true},
// 		{"IPv4 Private Class C", "192.168.1.100", true},
// 		{"Link-Local / AWS Metadata", "169.254.169.254", true},
// 		{"IPv6 Loopback", "::1", true},

// 		// Allowed Networks (Should return false)
// 		{"Public IPv4 Google DNS", "8.8.8.8", false},
// 		{"Public IPv4 Cloudflare", "1.1.1.1", false},
// 		{"Public IPv4 Example.com", "93.184.216.34", false},
// 		{"Public IPv6 Google", "2001:4860:4860::8888", false},

// 		// Edge Cases
// 		{"Nil IP", "", false},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			ip := net.ParseIP(tt.ipStr)

// 			got := IsBlockedIP(ip)
// 			if got != tt.expected {
// 				t.Errorf("IsBlockedIP(%q) = %v; want %v", tt.ipStr, got, tt.expected)
// 			}
// 		})
// 	}
// }

func TestMakeCall(t *testing.T) {
	tests := []struct {
		name        string
		address     string
		wantErr     bool
		errContains string
	}{
		// Success cases
		{
			name:    "Allowed public IPv4",
			address: "8.8.8.8:443",
			wantErr: false,
		},
		{
			name:    "Allowed public IPv6",
			address: "[2001:4860:4860::8888]:443",
			wantErr: false,
		},

		// Blocked network errors
		{
			name:        "Blocked local IPv4",
			address:     "127.0.0.1:80",
			wantErr:     true,
			errContains: "blocked network",
		},
		{
			name:        "Blocked AWS Metadata",
			address:     "169.254.169.254:80",
			wantErr:     true,
			errContains: "blocked network",
		},
		{
			name:        "Blocked local IPv6",
			address:     "[::1]:8080",
			wantErr:     true,
			errContains: "blocked network",
		},

		// Parsing errors
		{
			name:        "Missing port",
			address:     "8.8.8.8",
			wantErr:     true,
			errContains: "parsing address",
		},
		{
			name:        "Invalid IP",
			address:     "not-an-ip:80",
			wantErr:     true,
			errContains: "ip nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can pass an empty network string and nil for syscall.RawConn
			// because MakeCall currently only validates the address string.
			err := MakeCall("tcp", tt.address, nil)

			if (err != nil) != tt.wantErr {
				t.Errorf("MakeCall() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify that the error message contains the expected reason
			if err != nil && tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("MakeCall() error = %v, expected to contain %q", err, tt.errContains)
			}
		})
	}
}
