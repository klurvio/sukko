package gateway

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rs/zerolog"

	"github.com/klurvio/sukko/internal/shared/auth"
)

func testGatewayWithPermissions(t *testing.T) *Gateway {
	t.Helper()
	return &Gateway{
		permissions: NewPermissionChecker(
			[]string{"*.trade", "*.metadata"}, // public
			[]string{"inbox.{principal}"},     // user-scoped
			[]string{"rooms.{group_id}"},      // group-scoped
		),
		logger: zerolog.Nop(),
	}
}

func TestFilterSubscribeChannels_ValidChannelsPassThrough(t *testing.T) {
	t.Parallel()
	gw := testGatewayWithPermissions(t)

	claims := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"}, TenantID: "acme"}
	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"acme.BTC.trade", "acme.ETH.metadata"},
		"acme", claims)

	if len(result) != 2 {
		t.Errorf("expected 2 channels, got %d: %v", len(result), result)
	}
}

func TestFilterSubscribeChannels_WrongTenantFiltered(t *testing.T) {
	t.Parallel()
	gw := testGatewayWithPermissions(t)

	claims := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"}, TenantID: "acme"}
	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"acme.BTC.trade", "other.BTC.trade"},
		"acme", claims)

	if len(result) != 1 {
		t.Errorf("expected 1 channel (wrong tenant filtered), got %d: %v", len(result), result)
	}
	if len(result) > 0 && result[0] != "acme.BTC.trade" {
		t.Errorf("expected acme.BTC.trade, got %s", result[0])
	}
}

func TestFilterSubscribeChannels_InvalidFormatFiltered(t *testing.T) {
	t.Parallel()
	gw := testGatewayWithPermissions(t)

	claims := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"}, TenantID: "acme"}
	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"acme", "acme.BTC.trade"},
		"acme", claims)

	if len(result) != 1 {
		t.Errorf("expected 1 channel (single-part filtered), got %d: %v", len(result), result)
	}
}

func TestFilterSubscribeChannels_NilClaims_PublicOnly(t *testing.T) {
	t.Parallel()
	gw := testGatewayWithPermissions(t)

	// nil claims = API-key-only → public channels only
	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"acme.BTC.trade", "acme.inbox.user1"},
		"acme", nil)

	// BTC.trade matches *.trade (public), inbox.user1 matches inbox.{principal} (user-scoped, needs JWT)
	if len(result) != 1 {
		t.Errorf("expected 1 public channel, got %d: %v", len(result), result)
	}
	if len(result) > 0 && result[0] != "acme.BTC.trade" {
		t.Errorf("expected acme.BTC.trade, got %s", result[0])
	}
}

func TestFilterSubscribeChannels_JWTClaims_PublicAndUserScoped(t *testing.T) {
	t.Parallel()
	gw := testGatewayWithPermissions(t)

	claims := &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user1"}, TenantID: "acme"}
	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"acme.BTC.trade", "acme.inbox.user1"},
		"acme", claims)

	if len(result) != 2 {
		t.Errorf("expected 2 channels (public + user-scoped), got %d: %v", len(result), result)
	}
}

func TestFilterSubscribeChannels_AllFiltered_EmptyResult(t *testing.T) {
	t.Parallel()
	gw := testGatewayWithPermissions(t)

	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"other.BTC.trade", "wrong.ETH.trade"},
		"acme", nil)

	if len(result) != 0 {
		t.Errorf("expected 0 channels (all wrong tenant), got %d: %v", len(result), result)
	}
}

func TestFilterSubscribeChannels_NilPermissionChecker_AllPass(t *testing.T) {
	t.Parallel()
	// Auth disabled — no permission checker
	gw := &Gateway{
		permissions: nil,
		logger:      zerolog.Nop(),
	}

	result := gw.filterSubscribeChannels(context.Background(),
		[]string{"any.channel", "whatever.foo"},
		"acme", nil)

	// All channels pass when auth disabled (FR-011)
	if len(result) != 2 {
		t.Errorf("expected 2 channels (auth disabled, all pass), got %d: %v", len(result), result)
	}
}
