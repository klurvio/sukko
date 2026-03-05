# Authentication System

This document describes the authentication system used in the Sukko API.

## Overview

The API uses **JWT tokens** with **Internet Computer (IC) Principal-based authentication**. Users authenticate by signing a timestamp with their Ed25519 private key, and the server issues a JWT token upon successful verification.

## Table of Contents

- [JWT Token Structure](#jwt-token-structure)
- [Authentication Flow](#authentication-flow)
- [Token Validation](#token-validation)
- [Guards](#guards)
- [Decorators](#decorators)
- [Role-Based Access Control](#role-based-access-control)
- [API Key Authentication](#api-key-authentication)
- [Refresh Tokens](#refresh-tokens)
- [Configuration](#configuration)
- [Cryptography](#cryptography)

---

## Cryptography

The authentication system uses **two separate cryptographic mechanisms**:

### Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         AUTHENTICATION REQUEST                               │
│                              (POST /auth)                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│   Client Side (Ed25519 Asymmetric)         Server Side                      │
│   ─────────────────────────────────        ───────────────────────────      │
│                                                                              │
│   ┌─────────────┐                          ┌─────────────────────────┐      │
│   │ Private Key │──signs timestamp──────▶  │ Verify signature using  │      │
│   └─────────────┘                          │ client's public key     │      │
│                                            └───────────┬─────────────┘      │
│   ┌─────────────┐                                      │                    │
│   │ Public Key  │──sent to server───────▶  Derive principal from pubkey    │
│   └─────────────┘                                      │                    │
│                                                        ▼                    │
│                                            ┌─────────────────────────┐      │
│                                            │ Sign JWT with           │      │
│                                            │ JWT_SECRET (HS256)      │      │
│                                            └───────────┬─────────────┘      │
│                                                        │                    │
│                                                        ▼                    │
│                                            Return JWT token to client       │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1. Login Signature Verification (Ed25519 Asymmetric)

Used during authentication (`POST /auth`) to verify the user's identity.

| Component | Location | Purpose |
|-----------|----------|---------|
| **Private Key** | Client only | Signs the timestamp |
| **Public Key** | Sent to server | Verifies signature, derives principal |

```typescript
// Server verifies using client's public key
const isSignatureValid = Ed25519KeyIdentity.verify(
  new TextEncoder().encode(`${timestamp}`),
  decodedSignature,
  Ed25519PublicKey.fromDer(signingPubKey).rawKey,
);

// Principal derived from public key
const principal = Principal.selfAuthenticating(signingPubKey);
```

The server **never** has access to the client's private key.

### 2. JWT Token Signing (HS256 Symmetric)

Used for signing and verifying JWT tokens after authentication.

| Component | Location | Purpose |
|-----------|----------|---------|
| **JWT_SECRET** | Server only | Signs & verifies access tokens |
| **JWT_REFRESH_SECRET** | Server only | Signs & verifies refresh tokens |

```typescript
// Server signs JWT with symmetric secret
const token = this.jwtService.sign(
  { user: principal },
  { expiresIn: '24h' }
);

// Server verifies JWT with same secret
const decoded = this.jwtService.verify(token);
```

**Algorithm:** HS256 (HMAC-SHA256) - symmetric, same secret for sign/verify.

### Key Summary

| Key | Type | Algorithm | Stored On | Purpose |
|-----|------|-----------|-----------|---------|
| Client Private Key | Asymmetric | Ed25519 | Client | Sign login request |
| Client Public Key | Asymmetric | Ed25519 | Client (sent to server) | Verify login, derive principal |
| `JWT_SECRET` | Symmetric | HS256 | Server | Sign/verify access tokens |
| `JWT_REFRESH_SECRET` | Symmetric | HS256 | Server | Sign/verify refresh tokens |

### Why Two Mechanisms?

1. **Ed25519 (Login)**: Proves the user controls their IC identity without sending a password. The private key never leaves the client.

2. **HS256 (JWT)**: Efficient symmetric signing for subsequent API requests. The server can quickly verify tokens without public key cryptography overhead.

---

## JWT Token Structure

### Payload

```typescript
{
  user: string;   // IC Principal (user identity)
  iat: number;    // Issued at (Unix timestamp)
  exp: number;    // Expiration (Unix timestamp)
}
```

### Default Expiration

| Token Type | Duration | Environment Variable |
|------------|----------|---------------------|
| Access Token | 24 hours | `JWT_EXPIRES_IN` |
| Refresh Token | 7 days | `JWT_REFRESH_EXPIRES_IN` |

---

## Authentication Flow

### Step 1: Authenticate

```http
POST /auth
Content-Type: application/json

{
  "publickey": "<base64-encoded-public-key>",
  "timestamp": "1705276800000",
  "signature": "<base64-encoded-signature>",
  "referrer": "<optional-referrer-principal>"
}
```

**Alternative with Delegation Chain:**
```json
{
  "delegation": "<json-delegation-chain>",
  "timestamp": "1705276800000",
  "signature": "<base64-encoded-signature>"
}
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refreshToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

### Validation Rules

1. Timestamp must be within **5 minutes** of server time
2. Signature is verified using **Ed25519**
3. Principal is derived from the public key
4. If delegation chain is provided, the chain is validated

### Step 2: Use Token

Include the token in the `Authorization` header:

```http
GET /protected-endpoint
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

### Step 3: Verify Token (Optional)

```http
GET /auth
Authorization: Bearer <token>
```

**Response:** Returns the principal string if valid.

---

## Token Validation

The `AuthGuard` validates tokens using the following process:

```
┌─────────────────────────────────────────────────────────┐
│                    Request Received                      │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│           Extract Bearer Token from Header               │
│              Authorization: Bearer <token>               │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│              JwtService.verify(token)                    │
│         - Validates signature against JWT_SECRET         │
│         - Checks expiration (unless dev mode)            │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│              Check User Exists in Database               │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│     Check access_allowed (if access_required enabled)    │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│           Validate Roles (if @Roles decorator)           │
└─────────────────────────┬───────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│     Attach user to request.user (and _user if needed)    │
└─────────────────────────┴───────────────────────────────┘
```

---

## Guards

Four authentication guards are available:

### AuthGuard

The primary guard for protected routes.

```typescript
@UseGuards(AuthGuard)
@Get('protected')
async protectedRoute(@Principal() principal: string) {
  return { principal };
}
```

**Behavior:**
- Requires valid JWT token
- Validates user exists in database
- Enforces RBAC if `@Roles()` decorator present
- In development mode, token expiration is ignored

### TestAuthGuard

Development/testing guard that allows bypassing authentication.

```typescript
// Allows: GET /endpoint?principal=test-principal-123
```

**Behavior:**
- Accepts `principal` query parameter to mock a user
- Falls back to `AuthGuard` if no query param provided
- Only active in development environment

### AnonAuthGuard

Supports API key authentication alongside JWT.

```typescript
@UseGuards(AnonAuthGuard)
@Post('upload')
async upload() { }
```

**Behavior:**
- Checks `x-authorization` header for API key
- Validates API key against database
- Falls back to `AuthGuard` if no API key
- API key can be associated with a user or be anonymous

### OptionalAuthGuard

Makes authentication optional.

```typescript
@UseGuards(OptionalAuthGuard)
@Get('public')
async publicRoute(@Principal() principal?: string) {
  // principal is undefined if no token provided
}
```

**Behavior:**
- Returns `true` if no auth header (allows access)
- Validates token if provided
- Sets `request.user` only if valid token present

---

## Decorators

### @BioniqAuthGuard(options?)

Main decorator that selects the appropriate guard.

```typescript
// Basic usage
@BioniqAuthGuard()

// With API key support
@BioniqAuthGuard({ allowAnon: true })

// Optional authentication
@BioniqAuthGuard({ useOptional: true })
```

**Options:**
| Option | Type | Description |
|--------|------|-------------|
| `allowAnon` | `boolean` | Enables API key authentication |
| `useOptional` | `boolean` | Makes authentication optional |

**Environment Behavior:**
- Production: Uses `AuthGuard`
- Development: Uses `TestAuthGuard` (allows `?principal=` bypass)

### @Principal()

Extracts the user principal from the request.

```typescript
@Get()
async getUser(@Principal() principal: string) {
  return this.userService.findByPrincipal(principal);
}
```

### @UserObject()

Extracts the full user entity (requires `@GetUserObject()`).

```typescript
@BioniqAuthGuard()
@GetUserObject()
@Get()
async getUser(@UserObject() user: UserEntity) {
  return user;
}
```

### @GetUserObject()

Signals the guard to fetch and attach the full user object.

```typescript
@BioniqAuthGuard()
@GetUserObject()
@Get('profile')
async getProfile(@UserObject() user: UserEntity) {
  // user is the full database entity
}
```

### @Roles(...roles)

Restricts access based on user roles.

```typescript
@BioniqAuthGuard()
@Roles(Role.SuperAdmin)
@Delete(':id')
async delete(@Param('id') id: string) {
  // Only SuperAdmin can access
}
```

---

## Role-Based Access Control

### Role Hierarchy

```typescript
enum Role {
  User = 0,        // Default user
  Support = 5,     // Support staff
  SuperAdmin = 9,  // Administrator
}
```

The user's `admin` field in the database determines their role level. Access is granted if the user's level is **greater than or equal to** the required role.

### Usage Examples

```typescript
// Only support staff and above
@Roles(Role.Support)

// Only super admins
@Roles(Role.SuperAdmin)

// Multiple roles (any match grants access)
@Roles(Role.Support, Role.SuperAdmin)
```

### Database Field

```prisma
model User {
  admin Int @default(0)  // Role level
}
```

---

## API Key Authentication

API keys provide an alternative authentication method for programmatic access.

### Header

```http
x-authorization: <api-key>
```

### Database Schema

```prisma
model ApiKey {
  id        String   @id
  key       String   @unique
  user      String?  // Optional: associated user principal
  createdAt DateTime @default(now())
}
```

### Usage

```typescript
@BioniqAuthGuard({ allowAnon: true })
@Post('webhook')
async handleWebhook(@Principal() principal?: string) {
  // principal is set if API key is associated with a user
}
```

---

## Refresh Tokens

### Endpoint

```http
POST /auth/refresh
Content-Type: application/json

{
  "refreshToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

### Flow

1. Client stores both `token` and `refreshToken` from initial auth
2. When access token expires, send refresh token to `/auth/refresh`
3. Server validates refresh token using `JWT_REFRESH_SECRET`
4. Returns new access token
5. Refresh token is **not renewed** (use original until it expires)

### Requirements

Refresh tokens require `JWT_REFRESH_SECRET` environment variable to be set.

---

## Configuration

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `JWT_SECRET` | Yes | - | Symmetric secret for signing access tokens (HS256) |
| `JWT_REFRESH_SECRET` | No | - | Symmetric secret for signing refresh tokens (HS256) |
| `JWT_EXPIRES_IN` | No | `24h` | Access token expiration |
| `JWT_REFRESH_EXPIRES_IN` | No | `7d` | Refresh token expiration |

### Generating Secrets

```bash
# Generate a secure random secret (recommended: 256 bits / 32 bytes)
openssl rand -base64 32
```

Use different secrets for `JWT_SECRET` and `JWT_REFRESH_SECRET`.

### Module Configuration

```typescript
// app.module.ts
JwtModule.register({
  secret: process.env.JWT_SECRET,
  global: true,
})
```

**Note:** The module uses HS256 (symmetric) algorithm by default. No public/private key pair is needed for JWT signing.

---

## Key Files

| File | Description |
|------|-------------|
| `src/auth/auth.guard.ts` | Guard implementations |
| `src/auth/auth.controller.ts` | Auth endpoints |
| `src/auth/auth.decorator.ts` | Custom decorators |
| `src/app.module.ts` | JWT module config |

---

## Security Considerations

1. **Development Mode**: Token expiration is ignored - never use in production
2. **JWT Signing**: Uses HS256 symmetric algorithm - keep `JWT_SECRET` secure and never expose it
3. **Ed25519 Verification**: Client private keys never leave the client; server only sees public keys
4. **Separate Secrets**: Use different secrets for access tokens (`JWT_SECRET`) and refresh tokens (`JWT_REFRESH_SECRET`)
5. **Access Control**: Optional `access_required` setting enforces explicit user approval
6. **API Keys**: Validated on each request against database
7. **Cache Headers**: Auth endpoints return `no-cache, no-store` headers
8. **Timestamp Validation**: Login requests must be within 5 minutes to prevent replay attacks

---

## Related Documentation

| Document | Description |
|----------|-------------|
| [JWT Asymmetric Migration](./JWT_ASYMMETRIC_MIGRATION.md) | Migration guide from HS256 to ES256/RS256 for multi-tenant support |
| [Multi-Tenant Auth Design](../architecture/MULTI_TENANT_AUTH.md) | WS Gateway multi-tenant authentication architecture |
