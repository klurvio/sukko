# Plan: Align GKE provisioning with local (API-based)

## Context

The GKE `provision:create` task uses raw `rpk topic create` commands, bypassing the provisioning service entirely:
- Only 1 topic created (`trade`) vs 8 categories locally
- No tenant record created in the database
- No channel rules set
- No audit logging

The local environment uses the **Provisioning API** (`taskfiles/local.yml:129-185`) which creates tenant + topics + channel rules in a single workflow. Now that the provisioning service is running in GKE, the k8s tasks should use the same API approach.

## File to modify

`taskfiles/k8s.yml`

---

## Change 1: Update ports to avoid conflicts

In the `vars:` section, change the existing PostgreSQL port and add the provisioning port.
Both use a `4` prefix to avoid conflicts while clearly mapping to their service ports (5432, 8080).

**Change:**
```yaml
  K8S_PG_LOCAL_PORT: 54321
```
**To:**
```yaml
  K8S_PG_LOCAL_PORT: 45432
  K8S_PROVISIONING_LOCAL_PORT: 48080
```

---

## Change 2: Rewrite `provision:create`

**Current:**
```yaml
  provision:create:
    desc: Create topic + tenant + categories
    cmds:
      - kubectl exec -n {{.K8S_NAMESPACE}} {{.K8S_RELEASE_NAME}}-redpanda-0 -- rpk topic create {{.K8S_NAMESPACE}}.sukko.trade -p 3 2>/dev/null || true
      - 'echo "Topic ready: {{.K8S_NAMESPACE}}.sukko.trade"'
```

**Replace with:**
```yaml
  provision:create:
    desc: Create sukko tenant + Kafka topics + channel rules
    cmds:
      - |
        lsof -ti:{{.K8S_PROVISIONING_LOCAL_PORT}} | xargs kill 2>/dev/null || true
        sleep 1
        kubectl port-forward -n {{.K8S_NAMESPACE}} svc/{{.K8S_RELEASE_NAME}}-provisioning {{.K8S_PROVISIONING_LOCAL_PORT}}:8080 &
        PF_PID=$!
        sleep 2

        API="http://localhost:{{.K8S_PROVISIONING_LOCAL_PORT}}"

        # Wait for provisioning API
        echo "Checking provisioning API..."
        for i in $(seq 1 10); do
          if curl -sf $API/health >/dev/null 2>&1; then
            echo "Provisioning API ready"
            break
          fi
          echo "Waiting for provisioning API..."
          sleep 1
        done

        if ! curl -sf $API/health >/dev/null 2>&1; then
          echo "ERROR: Provisioning API not accessible at $API"
          kill $PF_PID 2>/dev/null || true
          exit 1
        fi

        # Create tenant
        echo "Creating tenant..."
        RESULT=$(curl -sf -X POST $API/api/v1/tenants \
          -H "Content-Type: application/json" \
          -d '{"id": "sukko", "name": "Sukko Trading Platform"}' 2>&1) || {
          if echo "$RESULT" | grep -q "already exists"; then
            echo "Tenant already exists, continuing..."
          else
            echo "Tenant creation response: $RESULT"
          fi
        }
        echo "$RESULT" | jq . 2>/dev/null || echo "$RESULT"

        # Create Kafka topics (same 8 categories as local)
        echo "Creating Kafka topics..."
        RESULT=$(curl -sf -X POST $API/api/v1/tenants/sukko/topics \
          -H "Content-Type: application/json" \
          -d '{"categories": ["trade", "liquidity", "metadata", "social", "community", "creation", "analytics", "balances"]}' 2>&1) || {
          echo "Topic creation failed: $RESULT"
        }
        echo "$RESULT" | jq . 2>/dev/null || echo "$RESULT"

        # Set channel rules
        echo "Setting channel rules..."
        RESULT=$(curl -sf -X PUT $API/api/v1/tenants/sukko/channel-rules \
          -H "Content-Type: application/json" \
          -d '{"public_patterns": ["*.metadata"], "user_scoped_patterns": ["balances.{subject}"], "group_scoped_patterns": []}' 2>&1) || {
          echo "Channel rules failed: $RESULT"
        }
        echo "$RESULT" | jq . 2>/dev/null || echo "$RESULT"

        echo "Provisioning complete"
        kill $PF_PID 2>/dev/null || true
```

---

## Change 3: Remove `provision:loadtest`

**Rationale:** Loadtest doesn't need separate per-asset topics (e.g. `BTC.trade`, `ETH.liquidity`).
The asset identifier belongs in the message payload, not the topic name. The 8 base categories
created by `provision:create` (`trade`, `liquidity`, `metadata`, etc.) are sufficient —
the loadtest publisher just publishes to those existing topics.

**Action:** Delete the `provision:loadtest` task entirely.

---

## Change 4: Rewrite `provision:status`

**Current:**
```yaml
  provision:status:
    desc: List topics + tenants
    cmds:
      - kubectl exec -n {{.K8S_NAMESPACE}} {{.K8S_RELEASE_NAME}}-redpanda-0 -- rpk topic list
```

**Replace with:**
```yaml
  provision:status:
    desc: Show tenants and topics via provisioning API
    cmds:
      - |
        lsof -ti:{{.K8S_PROVISIONING_LOCAL_PORT}} | xargs kill 2>/dev/null || true
        sleep 1
        kubectl port-forward -n {{.K8S_NAMESPACE}} svc/{{.K8S_RELEASE_NAME}}-provisioning {{.K8S_PROVISIONING_LOCAL_PORT}}:8080 &
        PF_PID=$!
        sleep 2
        API="http://localhost:{{.K8S_PROVISIONING_LOCAL_PORT}}"
        echo "=== Tenants ==="
        curl -s $API/api/v1/tenants | jq .
        echo ""
        echo "=== Topics (sukko) ==="
        curl -s $API/api/v1/tenants/sukko/topics | jq .
        echo ""
        echo "=== Channel Rules (sukko) ==="
        curl -s $API/api/v1/tenants/sukko/channel-rules | jq .
        kill $PF_PID 2>/dev/null || true
```

---

## Verification

```bash
task k8s:provision:create ENV=dev
# Expected: tenant created, 8 topics created, channel rules set

task k8s:provision:status ENV=dev
# Expected: tenants, topics, and channel rules JSON from API
```
