package main

import "testing"

func TestLoopbackOnly(t *testing.T) {
	for listen, want := range map[string]bool{
		"127.0.0.1:3001": true,
		"[::1]:3001":     true,
		"localhost:3001": true,
		":3001":          false,
		"0.0.0.0:3001":   false,
		"10.1.1.5:3001":  false,
		"bad":            false,
	} {
		if got := loopbackOnly(listen); got != want {
			t.Errorf("loopbackOnly(%q) = %v, want %v", listen, got, want)
		}
	}
}
