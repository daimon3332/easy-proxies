package monitor

import "testing"

func TestIsLoopbackListen(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1:9091": true,
		"[::1]:9091":     true,
		"localhost:9091": true,
		"0.0.0.0:9091":   false,
		"[::]:9091":      false,
		"192.0.2.1:9091": false,
		"invalid":        false,
	}
	for listen, want := range tests {
		if got := isLoopbackListen(listen); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", listen, got, want)
		}
	}
}
