package propus

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("default should be disabled")
	}
	if cfg.Port != 3710 {
		t.Fatalf("expected port 3710, got %d", cfg.Port)
	}
	if cfg.Bind != "loopback" {
		t.Fatalf("expected bind loopback, got %s", cfg.Bind)
	}
	if cfg.Tools != "coding" {
		t.Fatalf("expected tools coding, got %s", cfg.Tools)
	}
}

func TestListenAddr_Loopback(t *testing.T) {
	cfg := &Config{Port: 3710, Bind: "loopback"}
	addr := cfg.ListenAddr()
	if addr != "127.0.0.1:3710" {
		t.Fatalf("expected 127.0.0.1:3710, got %s", addr)
	}
}

func TestListenAddr_All(t *testing.T) {
	cfg := &Config{Port: 4000, Bind: "all"}
	addr := cfg.ListenAddr()
	if addr != "0.0.0.0:4000" {
		t.Fatalf("expected 0.0.0.0:4000, got %s", addr)
	}
}

func TestListenAddr_ZeroPort(t *testing.T) {
	cfg := &Config{Port: 0, Bind: "loopback"}
	addr := cfg.ListenAddr()
	if addr != "127.0.0.1:3710" {
		t.Fatalf("expected default port 3710, got %s", addr)
	}
}
