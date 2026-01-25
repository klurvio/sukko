# JWT Asymmetric Key Migration

**Date:** 2026-01-25
**Status:** Planned

---

## Overview

Migrate Odin API's JWT signing from **HS256 (symmetric)** to **RS256 or ES256 (asymmetric)** to support secure multi-tenant WebSocket authentication.

---

## Why This Change?

### Current State (HS256 - Symmetric)

```
┌─────────────────┐                      ┌─────────────────┐
│   Odin API      │                      │   WS Gateway    │
│                 │                      │                 │
│  JWT_SECRET     │ ◄── SAME KEY ──────► │  JWT_SECRET     │
│                 │                      │                 │
│  Signs JWT      │                      │  Verifies JWT   │
└─────────────────┘                      └─────────────────┘

Problem: Anyone with JWT_SECRET can forge tokens
```

### Target State (RS256/ES256 - Asymmetric)

```
┌─────────────────┐                      ┌─────────────────┐
│   Odin API      │                      │   WS Gateway    │
│                 │                      │                 │
│  PRIVATE KEY    │                      │  PUBLIC KEY     │
│  (signs)        │                      │  (verifies)     │
│                 │                      │                 │
│  Signs JWT      │      JWT Token       │  Verifies JWT   │
│  with private   │ ──────────────────►  │  with public    │
└─────────────────┘                      └─────────────────┘

Benefit: WS Gateway cannot forge tokens (no private key)
```

---

## What Changes

| Component | Current | Target |
|-----------|---------|--------|
| Algorithm | HS256 | RS256 or ES256 |
| Signing key | `JWT_SECRET` (shared) | Private key (Odin only) |
| Verification key | `JWT_SECRET` (shared) | Public key (shared with gateway) |
| JWT header | `{"alg":"HS256"}` | `{"alg":"RS256","kid":"odin-prod-2026"}` |
| JWT payload | `{user}` | `{sub, tenant_id}` |

---

## What Does NOT Change

- **User login flow**: Ed25519 signature verification stays the same
- **Principal derivation**: Still derived from user's Ed25519 public key
- **Refresh token flow**: Same logic, just different signing algorithm
- **API endpoints**: No changes to routes or request/response format

---

## Algorithm Choice: RS256 vs ES256

| Algorithm | Key Size | Signature Size | Performance | Recommendation |
|-----------|----------|----------------|-------------|----------------|
| **RS256** | 2048 bits | 256 bytes | Slower | More compatible |
| **ES256** | 256 bits | 64 bytes | Faster | Recommended |

**Recommendation:** Use **ES256** (ECDSA with P-256 curve) - smaller keys, smaller signatures, faster signing.

---

## Step-by-Step Migration

### Step 1: Generate Key Pair

```bash
# ES256 (ECDSA - recommended)
openssl ecparam -genkey -name prime256v1 -noout -out odin-private.pem
openssl ec -in odin-private.pem -pubout -out odin-public.pem

# Or RS256 (RSA - more compatible)
openssl genrsa -out odin-private.pem 2048
openssl rsa -in odin-private.pem -pubout -out odin-public.pem
```

### Step 2: Update NestJS JWT Module

**Current configuration:**

```typescript
// app.module.ts
import { JwtModule } from '@nestjs/jwt';

@Module({
  imports: [
    JwtModule.register({
      secret: process.env.JWT_SECRET,
      signOptions: { expiresIn: '24h' },
    }),
  ],
})
export class AppModule {}
```

**New configuration:**

```typescript
// app.module.ts
import { JwtModule } from '@nestjs/jwt';
import * as fs from 'fs';

@Module({
  imports: [
    JwtModule.register({
      privateKey: process.env.JWT_PRIVATE_KEY,
      publicKey: process.env.JWT_PUBLIC_KEY,
      signOptions: {
        algorithm: 'ES256',  // or 'RS256'
        expiresIn: '24h',
        keyid: process.env.JWT_KEY_ID || 'odin-prod-2026',
      },
      verifyOptions: {
        algorithms: ['ES256'],  // or ['RS256']
      },
    }),
  ],
})
export class AppModule {}
```

### Step 3: Update JWT Payload

**Current payload:**

```typescript
// auth.service.ts
const token = this.jwtService.sign({
  user: principal,
});
```

**New payload:**

```typescript
// auth.service.ts
const token = this.jwtService.sign({
  sub: principal,        // Standard claim (was: user)
  tenant_id: 'odin',     // NEW: tenant identifier
});
```

### Step 4: Update Token Verification (if needed)

**Current verification:**

```typescript
// auth.guard.ts
const decoded = this.jwtService.verify(token);
const principal = decoded.user;
```

**New verification:**

```typescript
// auth.guard.ts
const decoded = this.jwtService.verify(token);
const principal = decoded.sub;  // Changed from 'user' to 'sub'
```

### Step 5: Update Refresh Token (if using separate secret)

**Current:**

```typescript
// Uses JWT_REFRESH_SECRET (symmetric)
const refreshToken = this.jwtService.sign(
  { user: principal },
  { secret: process.env.JWT_REFRESH_SECRET, expiresIn: '7d' }
);
```

**New:**

```typescript
// Uses same private key with different expiry
const refreshToken = this.jwtService.sign(
  { sub: principal, tenant_id: 'odin', type: 'refresh' },
  { expiresIn: '7d' }
);
```

Or keep a separate key pair for refresh tokens if preferred.

---

## Environment Variables

### Current

| Variable | Purpose |
|----------|---------|
| `JWT_SECRET` | Sign & verify access tokens |
| `JWT_REFRESH_SECRET` | Sign & verify refresh tokens |

### New

| Variable | Purpose | Example |
|----------|---------|---------|
| `JWT_PRIVATE_KEY` | Sign tokens (PEM format) | `-----BEGIN EC PRIVATE KEY-----...` |
| `JWT_PUBLIC_KEY` | Verify tokens (PEM format) | `-----BEGIN PUBLIC KEY-----...` |
| `JWT_KEY_ID` | Key identifier in JWT header | `odin-prod-2026` |
| `JWT_ALGORITHM` | Signing algorithm | `ES256` or `RS256` |

### Loading Keys from Environment

For multi-line PEM keys in environment variables:

```typescript
// Option 1: Replace \n with actual newlines
const privateKey = process.env.JWT_PRIVATE_KEY.replace(/\\n/g, '\n');

// Option 2: Base64 encode the PEM
const privateKey = Buffer.from(process.env.JWT_PRIVATE_KEY_BASE64, 'base64').toString('utf8');

// Option 3: Load from file path
const privateKey = fs.readFileSync(process.env.JWT_PRIVATE_KEY_PATH, 'utf8');
```

---

## Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: odin-api-jwt-keys
  namespace: odin
type: Opaque
stringData:
  JWT_PRIVATE_KEY: |
    -----BEGIN EC PRIVATE KEY-----
    MHQCAQEEIBxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
    xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
    -----END EC PRIVATE KEY-----
  JWT_PUBLIC_KEY: |
    -----BEGIN PUBLIC KEY-----
    MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAExxxxxxxxxxxxxxxx
    xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
    -----END PUBLIC KEY-----
  JWT_KEY_ID: "odin-prod-2026"
```

Reference in deployment:

```yaml
env:
  - name: JWT_PRIVATE_KEY
    valueFrom:
      secretKeyRef:
        name: odin-api-jwt-keys
        key: JWT_PRIVATE_KEY
  - name: JWT_PUBLIC_KEY
    valueFrom:
      secretKeyRef:
        name: odin-api-jwt-keys
        key: JWT_PUBLIC_KEY
  - name: JWT_KEY_ID
    valueFrom:
      secretKeyRef:
        name: odin-api-jwt-keys
        key: JWT_KEY_ID
```

---

## WS Gateway Configuration

The WS Gateway only needs the **public key** (cannot sign, only verify).

```yaml
# Helm values for WS Gateway
ws-server:
  auth:
    enabled: true
    tenantKeys:
      - kid: "odin-prod-2026"
        tenantId: "odin"
        algorithm: "ES256"
        publicKey: |
          -----BEGIN PUBLIC KEY-----
          MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...
          -----END PUBLIC KEY-----
```

---

## JWT Token Structure

### Header (Before)

```json
{
  "alg": "HS256",
  "typ": "JWT"
}
```

### Header (After)

```json
{
  "alg": "ES256",
  "typ": "JWT",
  "kid": "odin-prod-2026"
}
```

### Payload (Before)

```json
{
  "user": "2vxsx-fae-xxxxxx",
  "iat": 1706000000,
  "exp": 1706086400
}
```

### Payload (After)

```json
{
  "sub": "2vxsx-fae-xxxxxx",
  "tenant_id": "odin",
  "iat": 1706000000,
  "exp": 1706086400
}
```

---

## Migration Strategy

### Option A: Hard Cutover (Simpler)

1. Generate key pair
2. Deploy Odin API with new keys
3. Deploy WS Gateway with public key
4. All existing tokens become invalid
5. Users must re-login

**Downtime:** Brief (users re-login)
**Complexity:** Low

### Option B: Dual Support (Zero Downtime)

1. Update WS Gateway to accept both HS256 and ES256
2. Deploy WS Gateway
3. Update Odin API to sign with ES256
4. Deploy Odin API
5. New tokens use ES256, old HS256 tokens still work until expiry
6. After 24h (token expiry), remove HS256 support from WS Gateway

**Downtime:** None
**Complexity:** Medium

### Recommendation

Use **Option A** (hard cutover) since:
- Token expiry is 24h (users login daily anyway)
- Simpler implementation
- Cleaner codebase (no dual-algorithm support)

---

## Verification Checklist

- [ ] Key pair generated securely
- [ ] Private key stored in Kubernetes secret
- [ ] Private key NOT committed to git
- [ ] Odin API signs with private key
- [ ] Odin API can verify its own tokens (optional)
- [ ] JWT header includes `kid`
- [ ] JWT payload includes `tenant_id`
- [ ] JWT payload uses `sub` instead of `user`
- [ ] WS Gateway has public key in config
- [ ] WS Gateway validates tokens successfully
- [ ] Refresh token flow works
- [ ] Old `JWT_SECRET` removed from environment

---

## Rollback Plan

If issues occur after migration:

1. Revert Odin API to previous version (HS256)
2. Revert WS Gateway to previous version
3. Restore `JWT_SECRET` environment variable
4. Users re-login to get new HS256 tokens

---

## Security Considerations

1. **Private key protection**: Never expose private key. Only Odin API should have it.
2. **Key rotation**: Plan for annual key rotation. Use `kid` to support multiple active keys.
3. **Algorithm restriction**: WS Gateway should only accept configured algorithms (ES256), reject others.
4. **Tenant verification**: WS Gateway should verify `tenant_id` in token matches expected tenant.

---

## Related Documentation

- [Authentication System](./README.md) - Current auth overview
- [Multi-Tenant Auth Design](../architecture/MULTI_TENANT_AUTH.md) - WS Gateway multi-tenant architecture
