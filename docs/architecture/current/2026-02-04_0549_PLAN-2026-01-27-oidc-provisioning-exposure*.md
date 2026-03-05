# Plan: OIDC/JWKS Support + Provisioning API Exposure

**Status:** ✅ Implemented

## Goal

Enable the ws-gateway and provisioning API to validate tokens from an external IdP (Auth0, Clerk, etc.) via OIDC JWKS, alongside existing per-tenant asymmetric keys from PostgreSQL. Expose the provisioning API with authentication, CORS, and a Helm subchart for K8s deployment.

**Key constraint**: Both IdP-issued and tenant-signed tokens use the same `Claims` struct (`tenant_id`, `roles`, `groups`, `scopes`). The gateway and provisioning API parse them identically.

---

## Part 1: OIDC Support in Auth Package

### 1.1 Add dependencies

```bash
cd ws && go get github.com/MicahParks/keyfunc/v3 github.com/go-chi/cors
```

`keyfunc/v3` is purpose-built for `golang-jwt/jwt/v5` (already in use). Provides auto-refreshing JWKS cache and returns `jwt.Keyfunc` directly.

### 1.2 New file: `ws/internal/auth/oidc.go`

OIDC keyfunc construction helper:

```go
type OIDCConfig struct {
    IssuerURL string  // e.g., "https://your-app.auth0.com/"
    JWKSURL   string  // e.g., "https://your-app.auth0.com/.well-known/jwks.json"
    Audience  string  // e.g., "https://api.sukko.io"
}

type OIDCKeyfuncResult struct {
    Keyfunc jwt.Keyfunc
    Close   func()       // Stops background JWKS refresh
}

func NewOIDCKeyfunc(ctx context.Context, cfg OIDCConfig, logger zerolog.Logger) (*OIDCKeyfuncResult, error)
```

- Wraps `keyfunc.NewDefault([]string{cfg.JWKSURL})`
- Returns the keyfunc + closer for lifecycle management
- Requires `JWKSURL`, returns error if empty

### 1.3 Add `ErrInvalidAudience` sentinel error

**File**: `ws/internal/auth/jwt.go`

Add `ErrInvalidAudience = errors.New("invalid audience")` to existing sentinel errors.

### 1.4 Extend `MultiTenantValidatorConfig` with OIDC fields

**File**: `ws/internal/auth/jwt_multitenant.go`

```go
type MultiTenantValidatorConfig struct {
    KeyRegistry       KeyRegistry       // existing
    RequireTenantID   bool              // existing
    RequireKeyID      bool              // existing
    AllowedAlgorithms []string          // existing
    OIDCIssuer        string            // NEW: expected issuer URL
    OIDCAudience      string            // NEW: expected audience
    OIDCKeyfunc       jwt.Keyfunc       // NEW: from NewOIDCKeyfunc()
}
```

Store these in the `MultiTenantValidator` struct as `oidcIssuer`, `oidcAudience`, `oidcKeyfunc`.

### 1.5 Modify `ValidateToken()` keyfunc for issuer-based routing

**File**: `ws/internal/auth/jwt_multitenant.go` (lines 75-120)

Inside the `jwt.ParseWithClaims()` keyfunc callback, the unverified `*jwt.Token` gives access to both `token.Header["kid"]` and `token.Claims` (including `Issuer`). Route based on issuer:

```
keyfunc(token):
  claims := token.Claims.(*Claims)
  if claims.Issuer == v.oidcIssuer AND v.oidcKeyfunc != nil:
    isOIDCToken = true
    return v.oidcKeyfunc(token)    // delegate to JWKS
  else:
    // existing flow: extract kid, look up in PostgreSQL key registry
```

After `ParseWithClaims` returns successfully, validate audience for OIDC tokens:

```go
if isOIDCToken && v.oidcAudience != "" {
    aud, err := claims.GetAudience()
    if err != nil || !slices.Contains(aud, v.oidcAudience) {
        return nil, fmt.Errorf("%w: expected %s", ErrInvalidAudience, v.oidcAudience)
    }
}
```

### 1.6 Tests

**File**: `ws/internal/auth/oidc_test.go` (new)
- `TestNewOIDCKeyfunc_MissingJWKSURL` — error when URL empty

**File**: `ws/internal/auth/jwt_multitenant_test.go` (append)
- `TestMultiTenantValidator_OIDCRouting` — table-driven: OIDC token with valid audience, wrong audience, tenant token with different issuer, tenant token with no issuer
- `TestMultiTenantValidator_OIDCDisabled_FallsBackToTenantKeys` — when OIDCKeyfunc is nil, all tokens use key registry
- `TestMultiTenantValidator_OIDCKeyfuncFailure_DoesNotBreakTenantKeys` — failing OIDC keyfunc only affects OIDC tokens

---

## Part 2: Gateway OIDC Configuration

### 2.1 Add OIDC config fields to `GatewayConfig`

**File**: `ws/internal/platform/gateway_config.go`

```go
OIDCEnabled   bool   `env:"OIDC_ENABLED" envDefault:"false"`
OIDCIssuerURL string `env:"OIDC_ISSUER_URL"`
OIDCAudience  string `env:"OIDC_AUDIENCE"`
OIDCJWKSURL   string `env:"OIDC_JWKS_URL"`
```

Add validation: when `OIDCEnabled=true`, require `OIDCIssuerURL` and `OIDCJWKSURL`.

### 2.2 Wire OIDC into `setupValidator()`

**File**: `ws/internal/gateway/gateway.go`

In `setupValidator()` (after creating `PostgresKeyRegistry`):

1. If `OIDCEnabled`, call `auth.NewOIDCKeyfunc()` with config
2. On failure: log warning, continue without OIDC (graceful degradation)
3. On success: pass `oidcKeyfunc`, `oidcIssuer`, `oidcAudience` to `MultiTenantValidatorConfig`

### 2.3 Add lifecycle management

**File**: `ws/internal/gateway/gateway.go`

- Add `oidcCloser *auth.OIDCKeyfuncResult` field to `Gateway` struct
- Call `oidcCloser.Close()` in `Gateway.Close()` to stop background JWKS refresh

---

## Part 3: Provisioning API Exposure

### 3.1 Add auth, OIDC, CORS config to `ProvisioningConfig`

**File**: `ws/internal/platform/provisioning_config.go`

```go
// Authentication
AuthEnabled bool `env:"AUTH_ENABLED" envDefault:"false"`

// OIDC
OIDCEnabled   bool   `env:"OIDC_ENABLED" envDefault:"false"`
OIDCIssuerURL string `env:"OIDC_ISSUER_URL"`
OIDCAudience  string `env:"OIDC_AUDIENCE"`
OIDCJWKSURL   string `env:"OIDC_JWKS_URL"`

// CORS
CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"http://localhost:3000"`
CORSMaxAge         int      `env:"CORS_MAX_AGE" envDefault:"3600"`
```

Validation:
- `AUTH_ENABLED=true` requires `DATABASE_URL` (reuses existing DB for key registry)
- `OIDC_ENABLED=true` requires `OIDC_ISSUER_URL` and `OIDC_JWKS_URL`

### 3.2 Add CORS middleware to provisioning router

**File**: `ws/internal/provisioning/api/router.go`

Add CORS fields to `RouterConfig`:
```go
CORSAllowedOrigins []string
CORSMaxAge         int
```

In `NewRouter()`, add `cors.Handler()` middleware **before** auth middleware (so preflight OPTIONS requests succeed without auth):

```go
r.Use(cors.Handler(cors.Options{
    AllowedOrigins:   cfg.CORSAllowedOrigins,
    AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
    AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
    ExposedHeaders:   []string{"X-Request-ID"},
    AllowCredentials: true,
    MaxAge:           cfg.CORSMaxAge,
}))
```

### 3.3 Wire auth + OIDC in `cmd/provisioning/main.go`

**File**: `ws/cmd/provisioning/main.go`

After existing DB setup:

1. If `AuthEnabled`: create `PostgresKeyRegistry` from existing `db` connection
2. If `OIDCEnabled`: create OIDC keyfunc (graceful degradation on failure)
3. Create `MultiTenantValidator` with both key sources
4. Pass validator, CORS config to `api.NewRouter()`

### 3.4 Tests

- CORS preflight test: `OPTIONS /api/v1/tenants` with `Origin` header returns proper `Access-Control-*` headers
- Auth disabled: API works without token (backward compatible)
- Auth enabled + OIDC: tokens validated via OIDC or tenant keys

---

## Part 4: Provisioning Helm Subchart

### 4.1 New subchart structure

```
deployments/k8s/helm/sukko/charts/provisioning/
  Chart.yaml
  values.yaml
  templates/
    _helpers.tpl
    deployment.yaml
    service.yaml
```

### 4.2 Key configuration

| Config | Default | Notes |
|--------|---------|-------|
| `enabled` | `true` | Toggle subchart |
| `replicaCount` | `1` | Single instance sufficient |
| `config.authEnabled` | `false` | Disabled for local dev |
| `config.oidcEnabled` | `false` | Disabled by default |
| `config.corsAllowedOrigins` | `http://localhost:3000` | Local dev default |
| `service.type` | `ClusterIP` | Override to `LoadBalancer` for external access |
| `service.port` | `8080` | |

Database URL sourced from the shared provisioning secret (same as gateway):
```yaml
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.global.provisioning.existingSecret | default (printf "%s-provisioning-db" .Release.Name) }}
      key: database-url
```

### 4.3 Add to parent chart

**File**: `deployments/k8s/helm/sukko/Chart.yaml` — add provisioning dependency
**File**: `deployments/k8s/helm/sukko/values.yaml` — add provisioning section

### 4.4 Dockerfile

**New file**: `ws/build/provisioning/Dockerfile`

Same pattern as `ws/build/gateway/Dockerfile`: multi-stage build, non-root user, expose 8080.

---

## Files Changed

### New Files (8)

| File | Purpose |
|------|---------|
| `ws/internal/auth/oidc.go` | `OIDCConfig`, `OIDCKeyfuncResult`, `NewOIDCKeyfunc()` |
| `ws/internal/auth/oidc_test.go` | Unit tests for OIDC keyfunc |
| `ws/build/provisioning/Dockerfile` | Docker build for provisioning |
| `charts/provisioning/Chart.yaml` | Helm chart definition |
| `charts/provisioning/values.yaml` | Default values |
| `charts/provisioning/templates/_helpers.tpl` | Template helpers |
| `charts/provisioning/templates/deployment.yaml` | K8s Deployment |
| `charts/provisioning/templates/service.yaml` | K8s Service |

### Modified Files (11)

| File | Changes |
|------|---------|
| `ws/go.mod` | Add `keyfunc/v3`, `go-chi/cors` |
| `ws/internal/auth/jwt.go` | Add `ErrInvalidAudience` |
| `ws/internal/auth/jwt_multitenant.go` | OIDC fields in config/struct, issuer routing, audience validation |
| `ws/internal/auth/jwt_multitenant_test.go` | OIDC routing + audience + fallback tests |
| `ws/internal/platform/gateway_config.go` | OIDC config fields + validation |
| `ws/internal/platform/provisioning_config.go` | Auth, OIDC, CORS config fields + validation |
| `ws/internal/gateway/gateway.go` | Wire OIDC in `setupValidator()`, add `oidcCloser` + `Close()` |
| `ws/internal/provisioning/api/router.go` | CORS middleware, CORS fields in `RouterConfig` |
| `ws/cmd/provisioning/main.go` | Wire auth, OIDC, CORS into startup |
| `deployments/k8s/helm/sukko/Chart.yaml` | Add provisioning dependency |
| `deployments/k8s/helm/sukko/values.yaml` | Add provisioning section |

---

## Implementation Order

```
Part 1 (Auth OIDC)
  ├──> Part 2 (Gateway OIDC Config)    ← depends on Part 1
  └──> Part 3 (Provisioning API)       ← depends on Part 1
         └──> Part 4 (Helm Subchart)   ← depends on Part 3
```

Parts 2 and 3 are independent of each other.

## Verification

```bash
cd ws
go build ./...                                           # compile check
go test ./... -count=1 -short                            # all tests pass
go test ./internal/auth/ -v -run TestMultiTenantValidator_OIDC  # OIDC-specific tests

# Helm
helm lint deployments/k8s/helm/sukko/charts/provisioning
helm lint deployments/k8s/helm/sukko
```

### Backward Compatibility
- All new config defaults to `false`/disabled
- `OIDC_ENABLED=false` (default): gateway + provisioning behave identically to before
- `AUTH_ENABLED=false` (default for provisioning): API works without auth
- Existing Helm values files need no changes
