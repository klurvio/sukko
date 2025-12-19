# Versioning Strategy - Comprehensive Plan

## Overview
This document outlines a comprehensive versioning strategy for the entire Odin WebSocket deployment stack, enabling safe evolution, easy rollbacks, and parallel version deployments.

## Problem Statement
When configuration, deployments, or application code changes in the future, we need:
1. Clear version boundaries for breaking changes
2. Ability to create v2, v3, etc. without breaking existing deployments
3. Docker image versioning tied to code versions
4. Configuration compatibility tracking
5. Rollback capability to previous versions
6. Environment-specific version pinning (dev, staging, prod)
7. Testing strategy per version
8. Documentation per version

## Versioning Philosophy

### Semantic Versioning (MAJOR.MINOR.PATCH)
- **MAJOR** (v1.0.0 → v2.0.0): Breaking changes
  - Configuration format changes (new env vars, removed vars, changed defaults)
  - API contract changes (WebSocket message format, HTTP endpoints)
  - Deployment structure changes (new services, removed services)
  - Kafka topic schema changes (new topics, changed formats)
  - Infrastructure requirements changes (resource limits, dependencies)

- **MINOR** (v1.0.0 → v1.1.0): Non-breaking additions
  - New features (additional metrics, new endpoints)
  - Performance improvements (optimizations, capacity increases)
  - New configuration options (with backward-compatible defaults)
  - Additional monitoring/observability

- **PATCH** (v1.0.0 → v1.0.1): Bug fixes
  - Security patches
  - Bug fixes
  - Documentation updates
  - Minor performance tweaks

## What Gets Versioned

### 1. Application Code
- **ws-server** (Go) - `/ws` directory
- **publisher** (TypeScript) - `/publisher` directory
- **Load testing** - `/loadtest` directory (tools version with stack)

### 2. Docker Images
- `odin-ws-server:v1.0.0`
- `odin-publisher:v1.0.0`

### 3. Configuration Stack
- **Shared configs** - `deployments/shared/`
- **Environment configs** - `deployments/local/`, `deployments/gcp/`
- **Monitoring configs** - Prometheus, Grafana, Loki configs

### 4. Deployment Manifests
- **docker-compose files** - All docker-compose.yml files
- **Deployment scripts** - GCP deployment automation
- **Taskfiles** - Task automation scripts

### 5. Documentation
- Capacity planning docs
- Architecture docs
- Deployment guides
- Migration guides (v1 → v2)

## Directory Structure Strategy

### Option A: Versioned Deployments Directory (RECOMMENDED)
```
odin-ws/
├── VERSION                           # Root version file
├── CHANGELOG.md                      # Version changelog
├── ws/                               # Application code (git versioned)
│   └── VERSION                       # ws-server version
├── publisher/                        # Application code (git versioned)
│   └── package.json                  # Already has version: "2.0.0"
├── deployments/
│   ├── v1/                          # Version 1 (current Kafka-based setup)
│   │   ├── VERSION                  # v1 metadata
│   │   ├── shared/
│   │   │   ├── base.env
│   │   │   ├── kafka-topics.env
│   │   │   └── ports.env
│   │   ├── local/
│   │   │   ├── docker-compose.yml
│   │   │   ├── overrides.env
│   │   │   └── .env.local           # Gitignored
│   │   └── gcp/
│   │       └── distributed/
│   │           ├── backend/
│   │           │   └── docker-compose.yml
│   │           └── ws-server/
│   │               ├── docker-compose.yml
│   │               ├── overrides.env
│   │               └── .env.production  # Gitignored
│   ├── v2/                          # Version 2 (future breaking changes)
│   │   ├── VERSION                  # v2 metadata
│   │   ├── MIGRATION.md             # v1 → v2 migration guide
│   │   └── [same structure as v1]
│   ├── current -> v1                # Symlink to current stable
│   └── old/                         # Archived NATS-based deployments
├── taskfiles/
│   ├── v1/                          # Version 1 tasks
│   │   ├── local/
│   │   ├── gcp/
│   │   └── shared/
│   ├── v2/                          # Version 2 tasks (future)
│   ├── current -> v1                # Symlink to current
│   └── version.yml                  # Version management tasks
├── docs/
│   ├── v1/                          # Version 1 documentation
│   │   ├── CAPACITY_PLANNING.md
│   │   ├── ARCHITECTURE.md
│   │   └── DEPLOYMENT_GUIDE.md
│   └── v2/                          # Version 2 documentation
└── .version-metadata.yml            # Version compatibility matrix
```

### Rationale for Versioned Directories
1. **Clear Isolation**: Each version is completely isolated
2. **Parallel Deployment**: Run v1 and v2 simultaneously for gradual rollout
3. **Easy Rollback**: Switch symlink `current -> v1` back if v2 fails
4. **Configuration Safety**: v2 config changes don't affect v1
5. **Testing**: Test v2 while v1 runs in production
6. **Migration Path**: Clear path from v1 → v2 with migration docs

## Docker Image Tagging Strategy

### Image Naming Convention
```
odin-ws-server:v{MAJOR}.{MINOR}.{PATCH}
odin-publisher:v{MAJOR}.{MINOR}.{PATCH}
```

### Multi-Tag Strategy
Each build creates 4 tags:
```bash
# Example: Building v1.2.3
docker build -t odin-ws-server:v1.2.3 .    # Specific version (immutable)
docker tag odin-ws-server:v1.2.3 odin-ws-server:v1.2    # Minor version (mutable)
docker tag odin-ws-server:v1.2.3 odin-ws-server:v1      # Major version (mutable)
docker tag odin-ws-server:v1.2.3 odin-ws-server:latest  # Latest stable (mutable)
```

### Usage in docker-compose
```yaml
# Production: Pin to specific patch version
ws-server:
  image: odin-ws-server:v1.2.3

# Staging: Pin to minor version (get patches)
ws-server:
  image: odin-ws-server:v1.2

# Development: Use major version or latest
ws-server:
  image: odin-ws-server:v1
```

## Version Metadata Files

### Root VERSION File
```yaml
# /VERSION
version: v1.0.0
release_date: 2025-11-05
release_name: "Kafka Migration"

components:
  ws-server: v1.0.0
  publisher: v2.0.0
  config-schema: v1
  deployment-manifests: v1

compatibility:
  min_ws_server: v1.0.0
  min_publisher: v2.0.0
  kafka_topics_version: v1
  
infrastructure:
  redpanda: v24.2.11
  prometheus: v3.6.0
  grafana: v12.2.0
  loki: v3.3.2

tested_environments:
  - name: local
    platform: Docker Desktop
    date: 2025-11-05
    status: passed
  - name: gcp-production
    platform: GCP e2-standard-4
    date: 2025-11-05
    status: passed

breaking_changes_from_previous:
  - "Migrated from NATS to Kafka/Redpanda"
  - "Changed configuration hierarchy (3-tier system)"
  - "New Prometheus metrics (kafka_* instead of nats_*)"
```

### ws/VERSION File
```yaml
# /ws/VERSION
version: v1.0.0
language: go
go_version: "1.25.1"
build_date: 2025-11-05

dependencies:
  franz-go: v1.18.0
  gobwas-ws: v1.4.0
  prometheus: v1.20.5
  zerolog: v1.33.0

features:
  - "12K WebSocket connections @ 1 CPU core"
  - "Kafka consumer with 96 partitions"
  - "Hierarchical channel subscriptions"
  - "Adaptive resource guards"
  - "Comprehensive Prometheus metrics"

api_version: v1
websocket_protocol_version: v1
```

### Compatibility Matrix (.version-metadata.yml)
```yaml
# /.version-metadata.yml
# Version compatibility and migration information

versions:
  v1.0.0:
    release_date: 2025-11-05
    status: stable
    components:
      ws-server: v1.0.0
      publisher: v2.0.0
      config-schema: v1
    config_files:
      - deployments/v1/shared/base.env
      - deployments/v1/shared/kafka-topics.env
    breaking_changes:
      - "Initial Kafka-based version"
    migration_from: null
    migration_guide: null

  v1.1.0:
    release_date: 2025-11-20  # Example future version
    status: development
    components:
      ws-server: v1.1.0
      publisher: v2.1.0
      config-schema: v1  # No breaking config changes
    config_files:
      - deployments/v1/shared/base.env  # Same config
    breaking_changes: []
    new_features:
      - "Enhanced metrics dashboard"
      - "Additional health check endpoints"
    migration_from: v1.0.0
    migration_guide: docs/v1/MIGRATION_v1.0_to_v1.1.md

  v2.0.0:
    release_date: TBD
    status: planned
    components:
      ws-server: v2.0.0
      publisher: v3.0.0
      config-schema: v2  # BREAKING: New config format
    config_files:
      - deployments/v2/shared/base.env  # New structure
    breaking_changes:
      - "New Kafka topic structure (hierarchical topics)"
      - "Changed WebSocket message format (added metadata)"
      - "New environment variables (removed WS_MAX_KAFKA_RATE)"
      - "Requires Redpanda v25+"
    migration_from: v1.x.x
    migration_guide: docs/v2/MIGRATION_v1_to_v2.md
    parallel_deployment: true  # Can run alongside v1

compatibility_matrix:
  ws-server:
    v1.0.0: ["publisher:v2.0.0", "config-schema:v1"]
    v1.1.0: ["publisher:v2.0.0", "publisher:v2.1.0", "config-schema:v1"]
    v2.0.0: ["publisher:v3.0.0", "config-schema:v2"]
  
  publisher:
    v2.0.0: ["ws-server:v1.0.0", "ws-server:v1.1.0"]
    v2.1.0: ["ws-server:v1.1.0"]
    v3.0.0: ["ws-server:v2.0.0"]
```

## Version Management Taskfile

### taskfiles/version.yml
```yaml
# Version management tasks
version: '3'

vars:
  VERSION_FILE: ./VERSION
  METADATA_FILE: ./.version-metadata.yml
  CURRENT_VERSION:
    sh: yq e '.version' {{.VERSION_FILE}}

tasks:
  info:
    desc: Show current version information
    cmds:
      - echo "Current Version - {{.CURRENT_VERSION}}"
      - yq e '.' {{.VERSION_FILE}}

  check:
    desc: Validate version consistency across all components
    cmds:
      - echo "Checking version consistency..."
      - |
        WS_VERSION=$(yq e '.version' ws/VERSION)
        PUB_VERSION=$(jq -r '.version' publisher/package.json)
        ROOT_WS=$(yq e '.components.ws-server' {{.VERSION_FILE}})
        ROOT_PUB=$(yq e '.components.publisher' {{.VERSION_FILE}})
        
        echo "ws/VERSION:              $WS_VERSION"
        echo "Root VERSION (ws):       $ROOT_WS"
        echo ""
        echo "publisher/package.json:  $PUB_VERSION"
        echo "Root VERSION (pub):      $ROOT_PUB"
        
        if [ "$WS_VERSION" != "$ROOT_WS" ] || [ "$PUB_VERSION" != "$ROOT_PUB" ]; then
          echo "❌ Version mismatch detected!"
          exit 1
        fi
        echo "✅ All versions consistent"

  bump:major:
    desc: Bump major version (breaking changes)
    prompt: This will create a new major version with breaking changes. Continue?
    cmds:
      - task: _create_version
        vars: {BUMP_TYPE: major}

  bump:minor:
    desc: Bump minor version (new features)
    cmds:
      - task: _create_version
        vars: {BUMP_TYPE: minor}

  bump:patch:
    desc: Bump patch version (bug fixes)
    cmds:
      - task: _create_version
        vars: {BUMP_TYPE: patch}

  _create_version:
    internal: true
    cmds:
      - echo "Bumping {{.BUMP_TYPE}} version..."
      - ./scripts/version-bump.sh {{.BUMP_TYPE}}
      - task: check
      - echo "✅ Version bumped successfully"

  tag:
    desc: Create git tag for current version
    cmds:
      - |
        VERSION={{.CURRENT_VERSION}}
        echo "Creating git tag: $VERSION"
        git tag -a $VERSION -m "Release $VERSION"
        echo "✅ Tag created. Push with: git push origin $VERSION"

  deploy:local:
    desc: Deploy specific version locally
    prompt: Which version do you want to deploy? (e.g., v1, v2)
    cmds:
      - |
        read -p "Version: " VERSION
        cd deployments/$VERSION/local
        docker-compose up -d
        echo "✅ Deployed $VERSION locally"

  rollback:
    desc: Rollback to previous version
    prompt: This will rollback to the previous version. Continue?
    cmds:
      - ./scripts/version-rollback.sh

  build:images:
    desc: Build Docker images for current version
    cmds:
      - task: _build_ws_image
      - task: _build_publisher_image

  _build_ws_image:
    internal: true
    cmds:
      - |
        VERSION={{.CURRENT_VERSION}}
        MAJOR=$(echo $VERSION | cut -d. -f1)
        MINOR=$(echo $VERSION | cut -d. -f1-2)
        
        echo "Building ws-server:$VERSION"
        cd ws
        docker build -t odin-ws-server:$VERSION .
        docker tag odin-ws-server:$VERSION odin-ws-server:$MINOR
        docker tag odin-ws-server:$VERSION odin-ws-server:$MAJOR
        docker tag odin-ws-server:$VERSION odin-ws-server:latest
        echo "✅ Built odin-ws-server with tags: $VERSION, $MINOR, $MAJOR, latest"

  _build_publisher_image:
    internal: true
    cmds:
      - |
        VERSION=$(jq -r '.version' publisher/package.json)
        MAJOR=$(echo v$VERSION | cut -d. -f1)
        MINOR=$(echo v$VERSION | cut -d. -f1-2)
        
        echo "Building publisher:v$VERSION"
        cd publisher
        docker build -t odin-publisher:v$VERSION .
        docker tag odin-publisher:v$VERSION odin-publisher:$MINOR
        docker tag odin-publisher:v$VERSION odin-publisher:$MAJOR
        docker tag odin-publisher:v$VERSION odin-publisher:latest
        echo "✅ Built odin-publisher with tags: v$VERSION, $MINOR, $MAJOR, latest"

  changelog:add:
    desc: Add entry to CHANGELOG.md
    cmds:
      - |
        VERSION={{.CURRENT_VERSION}}
        DATE=$(date +%Y-%m-%d)
        echo "" >> CHANGELOG.md
        echo "## $VERSION - $DATE" >> CHANGELOG.md
        echo "" >> CHANGELOG.md
        echo "### Added" >> CHANGELOG.md
        echo "- " >> CHANGELOG.md
        echo "" >> CHANGELOG.md
        echo "### Changed" >> CHANGELOG.md
        echo "- " >> CHANGELOG.md
        echo "" >> CHANGELOG.md
        echo "### Fixed" >> CHANGELOG.md
        echo "- " >> CHANGELOG.md
        echo "" >> CHANGELOG.md
        echo "✅ Added changelog entry for $VERSION"
        echo "Please edit CHANGELOG.md to add details"
```

## Migration Process (v1 → v2 Example)

### Step 1: Create v2 Directory Structure
```bash
# Copy v1 structure as starting point
cp -r deployments/v1 deployments/v2
cp -r taskfiles/v1 taskfiles/v2
cp -r docs/v1 docs/v2

# Update VERSION file in v2
echo "v2.0.0" > deployments/v2/VERSION
```

### Step 2: Make Breaking Changes in v2
```bash
# Edit v2 configurations
vim deployments/v2/shared/base.env
vim deployments/v2/shared/kafka-topics.env

# Update docker-compose files
vim deployments/v2/local/docker-compose.yml
```

### Step 3: Update Application Code
```bash
# Create v2 branch or tag
git checkout -b v2.0.0

# Update ws-server code
vim ws/config.go
vim ws/server.go

# Update ws VERSION file
echo "version: v2.0.0" > ws/VERSION
```

### Step 4: Create Migration Documentation
```markdown
# docs/v2/MIGRATION_v1_to_v2.md

## Breaking Changes
1. **Kafka Topic Structure**: Topics now use hierarchical naming
2. **Configuration**: Removed WS_MAX_KAFKA_RATE, added WS_KAFKA_CONSUMER_CONFIG
3. **WebSocket Protocol**: Messages now include metadata field

## Migration Steps
1. Deploy v2 alongside v1
2. Gradually route traffic to v2
3. Monitor v2 metrics
4. Decommission v1 after 24 hours

## Rollback Procedure
If issues arise:
1. Stop routing traffic to v2
2. Switch symlink: `ln -sf v1 deployments/current`
3. Restart services with v1
```

### Step 5: Test v2 Locally
```bash
# Deploy v2 locally
cd deployments/v2/local
docker-compose up -d

# Run tests
task test:v2:local
```

### Step 6: Deploy v2 to Production (Blue-Green)
```bash
# Deploy v2 on separate GCP instance
task deploy:gcp:v2

# Monitor metrics
task monitor:gcp:v2

# Switch traffic gradually (via load balancer)
# 10% → 50% → 100%

# If successful, update current symlink
ln -sf v2 deployments/current
```

### Step 7: Decommission v1
```bash
# After v2 is stable (e.g., 7 days)
task deploy:gcp:v1:down

# Archive v1 if needed
mv deployments/v1 deployments/archived/v1-$(date +%Y%m%d)
```

## Rollback Strategy

### Immediate Rollback (< 5 minutes)
```bash
# Switch symlink back
ln -sf v1 deployments/current

# Restart services
cd deployments/current/gcp/distributed/ws-server
docker-compose restart

# Verify
task health:check:all
```

### Database/State Rollback
If v2 introduced database schema changes:
```bash
# Run migration rollback script
./scripts/db-migrate-down.sh v2 v1

# Restore from backup if needed
./scripts/db-restore.sh backup-before-v2
```

## Testing Strategy Per Version

### Pre-Release Testing
```bash
# Run comprehensive tests on v2
task test:unit:v2
task test:integration:v2
task test:load:v2

# Compare v1 vs v2 performance
task benchmark:compare v1 v2
```

### Smoke Tests After Deployment
```bash
# Health checks
task health:check:v2

# Metrics validation
task metrics:validate:v2

# Connection test
task test:connections:v2 --connections=1000
```

### Monitoring During Rollout
```bash
# Monitor v2 dashboards
task monitor:grafana:v2

# Alert on anomalies
task alerts:watch:v2
```

## Version Promotion Flow

```
Development → Staging → Production

v1.1.0-dev    → v1.1.0-rc1    → v1.1.0
(local)         (staging GCP)    (production GCP)
```

### Promotion Commands
```bash
# Promote to staging
task version:promote:staging --version=v1.1.0

# Promote to production (after validation)
task version:promote:production --version=v1.1.0
```

## Documentation Structure Per Version

```
docs/
├── v1/
│   ├── ARCHITECTURE.md          # v1-specific architecture
│   ├── CAPACITY_PLANNING.md     # v1 capacity metrics
│   ├── DEPLOYMENT_GUIDE.md      # v1 deployment steps
│   ├── API.md                   # v1 API reference
│   └── TROUBLESHOOTING.md       # v1-specific issues
├── v2/
│   ├── MIGRATION_v1_to_v2.md   # Migration guide
│   ├── ARCHITECTURE.md          # v2 architecture changes
│   ├── CAPACITY_PLANNING.md     # v2 capacity updates
│   └── BREAKING_CHANGES.md      # Detailed breaking changes
└── README.md                    # Points to current version docs
```

## Implementation Checklist

### Phase 1: Setup Infrastructure
- [ ] Create VERSION file at root
- [ ] Create .version-metadata.yml
- [ ] Create CHANGELOG.md
- [ ] Add VERSION file to ws/
- [ ] Update publisher/package.json (already has version)

### Phase 2: Restructure Directories
- [ ] Move current deployments/ to deployments/v1/
- [ ] Move current taskfiles/ to taskfiles/v1/
- [ ] Create current symlinks
- [ ] Update all references in taskfiles to use versioned paths

### Phase 3: Create Version Management Tools
- [ ] Create taskfiles/version.yml
- [ ] Create scripts/version-bump.sh
- [ ] Create scripts/version-rollback.sh
- [ ] Create scripts/docker-build-versioned.sh

### Phase 4: Update CI/CD (if exists)
- [ ] Add version validation to CI
- [ ] Add automatic Docker image tagging
- [ ] Add version compatibility checks
- [ ] Add automated changelog generation

### Phase 5: Documentation
- [ ] Move current docs to docs/v1/
- [ ] Create migration template (docs/templates/MIGRATION.md)
- [ ] Create version compatibility matrix
- [ ] Update README.md with versioning strategy

### Phase 6: Testing
- [ ] Test version bump commands
- [ ] Test Docker image tagging
- [ ] Test deployment with versioned configs
- [ ] Test rollback procedure
- [ ] Document testing results

## Summary

This versioning strategy provides:
1. ✅ **Clear version boundaries** - Semantic versioning with isolated directories
2. ✅ **Easy v2 creation** - Copy v1, modify, deploy alongside
3. ✅ **Docker image versioning** - Multi-tag strategy for flexibility
4. ✅ **Configuration compatibility** - Tracked in metadata files
5. ✅ **Rollback capability** - Symlink switching + backup configs
6. ✅ **Environment-specific pinning** - Version per environment in docker-compose
7. ✅ **Testing strategy** - Comprehensive pre-release and smoke tests
8. ✅ **Documentation** - Version-specific docs with migration guides

**Next Step**: Implement Phase 1 (Setup Infrastructure) to establish the foundation.
