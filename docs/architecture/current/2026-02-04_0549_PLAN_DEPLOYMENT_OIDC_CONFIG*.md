# Plan: Update Deployments with OIDC/Channel Rules Config + Provisioning Tasks

## Summary

Update KinD (local) and GKE Standard (remote) deployment configurations with the new multi-issuer OIDC and per-tenant channel rules environment variables. Consolidate provisioning tasks (db migrations + API operations) into a single taskfile with namespaced tasks. Add automatic migration on deploy/rebuild.

---

## Part 1: Gateway Helm Chart Updates

### Files to Modify

| File | Purpose |
|------|---------|
| `deployments/k8s/helm/odin/charts/ws-gateway/values.yaml` | Add new config defaults |
| `deployments/k8s/helm/odin/charts/ws-gateway/templates/deployment.yaml` | Add env vars to pod |

### New Configuration Values to Add

```yaml
config:
  # Existing values...

  # Multi-Issuer OIDC (Feature Flag)
  multiIssuerOIDCEnabled: false

  # Per-Tenant Channel Rules (Feature Flag)
  perTenantChannelRulesEnabled: false

  # TenantRegistry Cache Settings
  issuerCacheTTL: "5m"
  channelRulesCacheTTL: "1m"
  oidcKeyfuncCacheTTL: "1h"

  # JWKS Fetch Settings
  jwksFetchTimeout: "10s"
  jwksRefreshInterval: "1h"

  # Fallback Channel Rules
  fallbackPublicChannels:
    - "*.metadata"
```

### New Environment Variables to Add in deployment.yaml

```yaml
- name: GATEWAY_MULTI_ISSUER_OIDC_ENABLED
  value: {{ .Values.config.multiIssuerOIDCEnabled | quote }}
- name: GATEWAY_PER_TENANT_CHANNEL_RULES
  value: {{ .Values.config.perTenantChannelRulesEnabled | quote }}
- name: GATEWAY_ISSUER_CACHE_TTL
  value: {{ .Values.config.issuerCacheTTL | quote }}
- name: GATEWAY_CHANNEL_RULES_CACHE_TTL
  value: {{ .Values.config.channelRulesCacheTTL | quote }}
- name: GATEWAY_OIDC_KEYFUNC_CACHE_TTL
  value: {{ .Values.config.oidcKeyfuncCacheTTL | quote }}
- name: GATEWAY_JWKS_FETCH_TIMEOUT
  value: {{ .Values.config.jwksFetchTimeout | quote }}
- name: GATEWAY_JWKS_REFRESH_INTERVAL
  value: {{ .Values.config.jwksRefreshInterval | quote }}
- name: GATEWAY_FALLBACK_PUBLIC_CHANNELS
  value: {{ .Values.config.fallbackPublicChannels | join "," | quote }}
```

---

## Part 2: Environment-Specific Values

### Local (KinD)

**File:** `deployments/k8s/helm/odin/values/local.yaml`

```yaml
ws-gateway:
  config:
    # Enable for local testing
    multiIssuerOIDCEnabled: false    # Can enable for testing
    perTenantChannelRulesEnabled: false
    # Shorter TTLs for development
    issuerCacheTTL: "30s"
    channelRulesCacheTTL: "10s"
```

### GKE Standard - Develop

**File:** `deployments/k8s/helm/odin/values/standard/develop.yaml`

```yaml
ws-gateway:
  config:
    multiIssuerOIDCEnabled: false    # Enable when ready to test
    perTenantChannelRulesEnabled: false
    issuerCacheTTL: "5m"
    channelRulesCacheTTL: "1m"
```

### GKE Standard - Staging

**File:** `deployments/k8s/helm/odin/values/standard/staging.yaml`

```yaml
ws-gateway:
  config:
    multiIssuerOIDCEnabled: false    # Enable for staging tests
    perTenantChannelRulesEnabled: false
    issuerCacheTTL: "5m"
    channelRulesCacheTTL: "1m"
```

### GKE Standard - Production

**File:** `deployments/k8s/helm/odin/values/standard/production.yaml`

```yaml
ws-gateway:
  config:
    multiIssuerOIDCEnabled: false    # Enable after staging validation
    perTenantChannelRulesEnabled: false
    issuerCacheTTL: "5m"
    channelRulesCacheTTL: "1m"
    oidcKeyfuncCacheTTL: "1h"
```

---

## Part 3: Consolidated Provisioning Taskfile

### Rationale

Consolidate all provisioning-related tasks into a single file with clear namespacing:
- `db:*` - Database migration tasks (Atlas)
- `api:*` - REST API client tasks (curl)

This avoids confusion between `provisioning.yml` and `provisioning-api.yml`.

### Updated File: `taskfiles/provisioning.yml`

```yaml
version: "3"

# Consolidated Provisioning Tasks
# - db:*  - Database migrations (Atlas)
# - api:* - REST API operations (curl)

vars:
  MIGRATIONS_DIR: "{{.ROOT_DIR}}/ws/internal/provisioning/repository/migrations"
  PROVISIONING_URL: '{{.PROVISIONING_URL | default "http://localhost:8082"}}'
  TENANT_ID: '{{.TENANT_ID | default "test-tenant"}}'

tasks:
  # ===========================================================================
  # Database Migrations (Atlas)
  # ===========================================================================
  db:migrate:
    desc: Apply database migrations
    summary: |
      Applies all pending database migrations using Atlas.
      Requires DATABASE_URL environment variable to be set.
    dir: "{{.ROOT_DIR}}"
    preconditions:
      - sh: command -v atlas
        msg: "Atlas CLI not installed. Install with: curl -sSf https://atlasgo.sh | sh"
      - sh: '[ -n "$DATABASE_URL" ]'
        msg: "DATABASE_URL environment variable not set"
    cmds:
      - atlas migrate apply --url "$DATABASE_URL" --dir "file://{{.MIGRATIONS_DIR}}"

  db:status:
    desc: Show migration status
    dir: "{{.ROOT_DIR}}"
    preconditions:
      - sh: command -v atlas
        msg: "Atlas CLI not installed. Install with: curl -sSf https://atlasgo.sh | sh"
      - sh: '[ -n "$DATABASE_URL" ]'
        msg: "DATABASE_URL environment variable not set"
    cmds:
      - atlas migrate status --url "$DATABASE_URL" --dir "file://{{.MIGRATIONS_DIR}}"

  db:validate:
    desc: Check for schema drift
    dir: "{{.ROOT_DIR}}"
    preconditions:
      - sh: command -v atlas
        msg: "Atlas CLI not installed. Install with: curl -sSf https://atlasgo.sh | sh"
      - sh: '[ -n "$DATABASE_URL" ]'
        msg: "DATABASE_URL environment variable not set"
    cmds:
      - atlas schema diff --from "$DATABASE_URL" --to "file://{{.MIGRATIONS_DIR}}"

  db:new:
    desc: Create a new migration file
    summary: |
      Usage: task provisioning:db:new -- migration_name
      Example: task provisioning:db:new -- add_user_preferences
    dir: "{{.ROOT_DIR}}"
    preconditions:
      - sh: command -v atlas
        msg: "Atlas CLI not installed. Install with: curl -sSf https://atlasgo.sh | sh"
    cmds:
      - atlas migrate new {{.CLI_ARGS}} --dir "file://{{.MIGRATIONS_DIR}}"

  db:hash:
    desc: Recalculate migration hash
    dir: "{{.ROOT_DIR}}"
    preconditions:
      - sh: command -v atlas
        msg: "Atlas CLI not installed. Install with: curl -sSf https://atlasgo.sh | sh"
    cmds:
      - atlas migrate hash --dir "file://{{.MIGRATIONS_DIR}}"

  db:lint:
    desc: Lint migration files for issues
    dir: "{{.ROOT_DIR}}"
    preconditions:
      - sh: command -v atlas
        msg: "Atlas CLI not installed. Install with: curl -sSf https://atlasgo.sh | sh"
    cmds:
      - atlas migrate lint --dir "file://{{.MIGRATIONS_DIR}}" --dev-url "docker://postgres/15"

  # ===========================================================================
  # API Operations (REST)
  # ===========================================================================
  # NOTE: All admin endpoints use /api/v1/admin prefix
  # ===========================================================================

  api:health:
    desc: Check provisioning API health
    cmds:
      - curl -s {{.PROVISIONING_URL}}/health | jq

  # --- Tenant Management ---
  api:tenant:create:
    desc: Create a tenant
    vars:
      TENANT_NAME: '{{.TENANT_NAME | default "Test Tenant"}}'
      CONTACT_EMAIL: '{{.CONTACT_EMAIL | default "test@example.com"}}'
    cmds:
      - |
        curl -s -X POST {{.PROVISIONING_URL}}/api/v1/admin/tenants \
          -H "Content-Type: application/json" \
          -d '{"name": "{{.TENANT_NAME}}", "contact_email": "{{.CONTACT_EMAIL}}"}' | jq

  api:tenant:list:
    desc: List all tenants
    cmds:
      - curl -s {{.PROVISIONING_URL}}/api/v1/admin/tenants | jq

  api:tenant:get:
    desc: Get tenant details
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}} | jq

  api:tenant:update:
    desc: Update tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - |
        curl -s -X PATCH {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}} \
          -H "Content-Type: application/json" \
          -d '{{.DATA}}' | jq

  # --- Topic Management ---
  api:topics:create:
    desc: Create topics for a tenant
    requires:
      vars: [TENANT_ID]
    vars:
      TOPICS: '{{.TOPICS | default "trade,liquidity,metadata,balances"}}'
    cmds:
      - |
        curl -s -X POST {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/topics \
          -H "Content-Type: application/json" \
          -d '{"topics": ["{{.TOPICS | replace "," "\",\""}}"] }' | jq

  api:topics:list:
    desc: List topics for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/topics | jq

  # --- OIDC Configuration ---
  api:oidc:create:
    desc: Create OIDC config for a tenant
    requires:
      vars: [TENANT_ID, ISSUER_URL]
    vars:
      AUDIENCE: '{{.AUDIENCE | default ""}}'
    cmds:
      - |
        curl -s -X POST {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/oidc-configs \
          -H "Content-Type: application/json" \
          -d '{"issuer_url": "{{.ISSUER_URL}}", "audience": "{{.AUDIENCE}}", "enabled": true}' | jq

  api:oidc:get:
    desc: Get OIDC config for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/oidc-configs | jq

  api:oidc:update:
    desc: Update OIDC config for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - |
        curl -s -X PUT {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/oidc-configs \
          -H "Content-Type: application/json" \
          -d '{{.DATA}}' | jq

  api:oidc:delete:
    desc: Delete OIDC config for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s -X DELETE {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/oidc-configs | jq

  # --- Channel Rules ---
  api:channel-rules:get:
    desc: Get channel rules for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/channel-rules | jq

  api:channel-rules:set:
    desc: Set channel rules for a tenant
    requires:
      vars: [TENANT_ID]
    vars:
      RULES: '{{.RULES | default `{"public_patterns": ["*.metadata"], "user_scoped_patterns": ["balances.{subject}"], "group_scoped_patterns": []}`}}'
    cmds:
      - |
        curl -s -X PUT {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/channel-rules \
          -H "Content-Type: application/json" \
          -d '{{.RULES}}' | jq

  api:channel-rules:delete:
    desc: Delete channel rules for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s -X DELETE {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/channel-rules | jq

  # --- Keys ---
  api:keys:create:
    desc: Create API key for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - |
        curl -s -X POST {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/keys \
          -H "Content-Type: application/json" \
          -d '{"name": "{{.KEY_NAME | default "default"}}"}' | jq

  api:keys:list:
    desc: List API keys for a tenant
    requires:
      vars: [TENANT_ID]
    cmds:
      - curl -s {{.PROVISIONING_URL}}/api/v1/admin/tenants/{{.TENANT_ID}}/keys | jq

  # --- Quick Setup ---
  api:quick-setup:
    desc: Quick setup - create tenant with topics and channel rules
    vars:
      TENANT_ID: '{{.TENANT_ID | default "test-tenant"}}'
      TENANT_NAME: '{{.TENANT_NAME | default "Test Tenant"}}'
    cmds:
      - task: api:tenant:create
        vars:
          TENANT_NAME: "{{.TENANT_NAME}}"
      - task: api:topics:create
        vars:
          TENANT_ID: "{{.TENANT_ID}}"
          TOPICS: "trade,liquidity,metadata,balances"
      - task: api:channel-rules:set
        vars:
          TENANT_ID: "{{.TENANT_ID}}"
      - echo "✓ Tenant {{.TENANT_ID}} created with topics and channel rules"
```

---

## Part 4: K8s Taskfile Updates with Auto-Migration

### Local (KinD): `taskfiles/k8s/local.yml`

Add migration to deploy and build:reload tasks:

```yaml
vars:
  # Add database URL for local postgres
  LOCAL_DB_URL: "postgres://postgres:postgres@localhost:5432/provisioning?sslmode=disable"

tasks:
  # Update existing deploy task
  deploy:
    desc: Deploy/upgrade Helm release (runs migrations first)
    cmds:
      - kubectl config use-context kind-{{.LOCAL_CLUSTER}}
      - task: migrate
      - task: common:asyncapi:bundle
      - helm upgrade --install odin {{.LOCAL_HELM_CHART}} -f {{.LOCAL_VALUES}} -n {{.LOCAL_NS}} --create-namespace

  # Update existing build:reload task
  build:reload:
    desc: Rebuild images, run migrations, and restart pods
    cmds:
      - task: build
      - task: migrate
      - kubectl rollout restart deployment -n {{.LOCAL_NS}}

  # Add migration task
  migrate:
    desc: Run database migrations (local)
    cmds:
      - |
        # Port-forward postgres if not already running
        if ! nc -z localhost 5432 2>/dev/null; then
          echo "Starting port-forward to postgres..."
          kubectl port-forward -n {{.LOCAL_NS}} svc/odin-provisioning-db 5432:5432 &
          sleep 2
        fi
      - DATABASE_URL="{{.LOCAL_DB_URL}}" task provisioning:db:migrate

  # Add provisioning API tasks
  port-forward:provisioning:
    desc: Port forward provisioning API
    cmds:
      - kubectl port-forward -n {{.LOCAL_NS}} svc/odin-provisioning 8082:8082

  provision:quick-setup:
    desc: Create test tenant with topics (local)
    vars:
      PROVISIONING_URL: "http://localhost:8082"
    cmds:
      - task: provisioning:api:quick-setup
        vars:
          PROVISIONING_URL: "{{.PROVISIONING_URL}}"
          TENANT_ID: "{{.TENANT_ID | default \"local-test\"}}"

  provision:tenant:list:
    desc: List tenants (local)
    vars:
      PROVISIONING_URL: "http://localhost:8082"
    cmds:
      - task: provisioning:api:tenant:list
        vars:
          PROVISIONING_URL: "{{.PROVISIONING_URL}}"
```

### GKE Standard: `taskfiles/k8s/standard.yml`

Add migration to deploy tasks:

```yaml
tasks:
  # Update existing deploy task
  deploy:
    desc: Deploy/upgrade Helm release (runs migrations first)
    vars:
      REDPANDA_IP:
        sh: terraform -chdir={{.GKE_STD_TF_DIR}} output -raw redpanda_external_ip 2>/dev/null || echo ""
    cmds:
      - task: connect
      - task: migrate
      - task: common:asyncapi:bundle
      - |
        HELM_CMD="helm upgrade --install odin {{.GKE_STD_CHART}} -f {{.GKE_STD_VALUES}} -n {{.GKE_STD_NS}} --create-namespace"
        if [ -n "{{.REDPANDA_IP}}" ]; then
          echo "Using Redpanda static IP: {{.REDPANDA_IP}}"
          HELM_CMD="$HELM_CMD --set redpanda.externalAccess.advertisedHost={{.REDPANDA_IP}} --set redpanda.externalAccess.loadBalancerIP={{.REDPANDA_IP}}"
        fi
        eval $HELM_CMD

  # Update existing build:reload task
  build:reload:
    desc: Rebuild all images, run migrations, and restart pods
    cmds:
      - task: build
      - task: migrate
      - kubectl rollout restart deployment -n {{.GKE_STD_NS}}

  # Add migration task
  migrate:
    desc: Run database migrations (GKE Standard)
    vars:
      DB_URL:
        sh: |
          # Get database URL from secret
          kubectl get secret odin-provisioning-db -n {{.GKE_STD_NS}} -o jsonpath='{.data.database-url}' | base64 -d
    cmds:
      - |
        echo "Running migrations for {{.GKE_STD_ENV}}..."
        DATABASE_URL="{{.DB_URL}}" task provisioning:db:migrate

  # Add provisioning API tasks
  provision:quick-setup:
    desc: Create test tenant with topics (GKE)
    vars:
      PROVISIONING_IP:
        sh: kubectl get svc odin-provisioning -n {{.GKE_STD_NS}} -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo ""
    cmds:
      - |
        if [ -z "{{.PROVISIONING_IP}}" ]; then
          echo "Provisioning service IP not available. Using port-forward..."
          kubectl port-forward -n {{.GKE_STD_NS}} svc/odin-provisioning 8082:8082 &
          sleep 2
          PROVISIONING_URL="http://localhost:8082"
        else
          PROVISIONING_URL="http://{{.PROVISIONING_IP}}:8082"
        fi
        task provisioning:api:quick-setup PROVISIONING_URL="$PROVISIONING_URL" TENANT_ID="{{.TENANT_ID | default (printf \"%s-test\" .GKE_STD_ENV)}}"

  provision:tenant:list:
    desc: List tenants (GKE)
    vars:
      PROVISIONING_IP:
        sh: kubectl get svc odin-provisioning -n {{.GKE_STD_NS}} -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || echo ""
    cmds:
      - |
        if [ -z "{{.PROVISIONING_IP}}" ]; then
          kubectl port-forward -n {{.GKE_STD_NS}} svc/odin-provisioning 8082:8082 &
          sleep 2
          PROVISIONING_URL="http://localhost:8082"
        else
          PROVISIONING_URL="http://{{.PROVISIONING_IP}}:8082"
        fi
        task provisioning:api:tenant:list PROVISIONING_URL="$PROVISIONING_URL"
```

---

## Part 5: Implementation Order

### Step 1: Update Helm Chart Base Values
1. Edit `deployments/k8s/helm/odin/charts/ws-gateway/values.yaml`
2. Add new config section with OIDC/channel rules settings

### Step 2: Update Deployment Template
1. Edit `deployments/k8s/helm/odin/charts/ws-gateway/templates/deployment.yaml`
2. Add new environment variables

### Step 3: Update Environment Values
1. Edit `deployments/k8s/helm/odin/values/local.yaml`
2. Edit `deployments/k8s/helm/odin/values/standard/develop.yaml`
3. Edit `deployments/k8s/helm/odin/values/standard/staging.yaml`
4. Edit `deployments/k8s/helm/odin/values/standard/production.yaml`

### Step 4: Consolidate Provisioning Taskfile
1. Update `taskfiles/provisioning.yml` with both `db:` and `api:` namespaces
2. Delete `taskfiles/provisioning-api.yml` if it exists (not needed)

### Step 5: Update K8s Taskfiles with Auto-Migration
1. Update `taskfiles/k8s/local.yml`:
   - Add `migrate` task
   - Update `deploy` to run migrations first
   - Update `build:reload` to run migrations
   - Add provisioning API convenience tasks
2. Update `taskfiles/k8s/standard.yml`:
   - Add `migrate` task
   - Update `deploy` to run migrations first
   - Update `build:reload` to run migrations
   - Add provisioning API convenience tasks

### Step 6: Update Main Taskfile (if needed)
1. Ensure provisioning tasks are accessible from root

---

## Verification

### Helm Template Validation
```bash
task k8s:common:helm:template ENV=local
# Verify new env vars appear in rendered output
```

### Local (KinD) Testing
```bash
# Deploy includes migration automatically
task k8s:local:deploy

# Create test tenant
task k8s:local:provision:quick-setup TENANT_ID=test-local

# Verify
task k8s:local:provision:tenant:list
task k8s:local:logs:gateway
```

### GKE Standard Testing
```bash
# Deploy includes migration automatically
task k8s:standard:deploy:develop

# Create test tenant
task k8s:standard:provision:quick-setup

# Verify
task k8s:standard:provision:tenant:list
```

### Migration Commands
```bash
# Check migration status
task provisioning:db:status

# Run migrations manually (if needed)
task k8s:local:migrate
task k8s:standard:migrate
```

---

## Files Summary

| File | Action |
|------|--------|
| `deployments/k8s/helm/odin/charts/ws-gateway/values.yaml` | Modify |
| `deployments/k8s/helm/odin/charts/ws-gateway/templates/deployment.yaml` | Modify |
| `deployments/k8s/helm/odin/values/local.yaml` | Modify |
| `deployments/k8s/helm/odin/values/standard/develop.yaml` | Modify |
| `deployments/k8s/helm/odin/values/standard/staging.yaml` | Modify |
| `deployments/k8s/helm/odin/values/standard/production.yaml` | Modify |
| `taskfiles/provisioning.yml` | Modify (consolidate db + api) |
| `taskfiles/k8s/local.yml` | Modify (add migrate, api tasks) |
| `taskfiles/k8s/standard.yml` | Modify (add migrate, api tasks) |

---

## Key Changes from Original Plan

1. **Consolidated taskfile**: Single `provisioning.yml` with `db:*` and `api:*` namespaces instead of separate files
2. **Fixed API endpoints**: All admin endpoints now use `/api/v1/admin/` prefix
3. **Fixed OIDC path**: Changed from `/oidc` to `/oidc-configs`
4. **Auto-migration**: `deploy` and `build:reload` tasks now run migrations automatically
5. **Path parameter**: Using `{tenantID}` consistently (matches router.go)
