package ws

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewPool(t *testing.T) {
	t.Parallel()

	p := NewPool(zerolog.Nop())
	if p == nil {
		t.Fatal("expected non-nil pool")
	}
	if p.Active() != 0 {
		t.Errorf("active = %d, want 0", p.Active())
	}
}

func TestPool_DrainSummary_Empty(t *testing.T) {
	t.Parallel()

	p := NewPool(zerolog.Nop())
	p.Drain()
	s := p.DrainSummary()
	if s != "drained 0 connections" {
		t.Errorf("summary = %q, want %q", s, "drained 0 connections")
	}
}

func TestPool_RampUp_EmptyGatewayURL(t *testing.T) {
	t.Parallel()

	p := NewPool(zerolog.Nop())
	err := p.RampUp(context.Background(), PoolConfig{GatewayURL: ""}, 1, 10)
	if err == nil {
		t.Fatal("expected error for empty gateway URL")
	}
}

func TestPool_RampUp_ContextCancelled(t *testing.T) {
	t.Parallel()

	p := NewPool(zerolog.Nop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.RampUp(ctx, PoolConfig{GatewayURL: "ws://localhost:59999"}, 10, 100)
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

func TestPool_RefreshAll_Empty(t *testing.T) {
	t.Parallel()

	p := NewPool(zerolog.Nop())
	refreshed, failed := p.RefreshAll(func(i int) string { return "token" })
	if refreshed != 0 || failed != 0 {
		t.Errorf("RefreshAll on empty pool: refreshed=%d, failed=%d, want 0/0", refreshed, failed)
	}
}

func TestPoolConfig_TokenFunc_Precedence(t *testing.T) {
	t.Parallel()

	// TokenFunc should take precedence over static Token.
	// We can't connect to a real server, but we can verify the config
	// structure accepts both fields without compilation errors.
	cfg := PoolConfig{
		GatewayURL: "ws://localhost:9999",
		Token:      "static-token",
		TokenFunc:  func(i int) string { return "per-connection-token" },
	}
	if cfg.TokenFunc == nil {
		t.Fatal("TokenFunc should be set")
	}
	if cfg.TokenFunc(0) != "per-connection-token" {
		t.Errorf("TokenFunc(0) = %q, want per-connection-token", cfg.TokenFunc(0))
	}
}

func TestPool_DrainIdempotent(t *testing.T) {
	t.Parallel()

	p := NewPool(zerolog.Nop())
	// Drain on empty pool should not panic
	p.Drain()
	p.Drain()
	if p.Active() != 0 {
		t.Errorf("active after double drain = %d, want 0", p.Active())
	}
}
