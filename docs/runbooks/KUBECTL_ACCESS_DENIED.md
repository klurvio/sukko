# Runbook: kubectl Access Denied (Connection Timeout)

## Symptom

```
E0113 10:22:25.109967 memcache.go:265] "Unhandled Error"
err="couldn't get current server API group list: dial tcp 34.71.66.27:443: i/o timeout"

Unable to connect to the server: dial tcp 34.71.66.27:443: i/o timeout
```

kubectl commands hang and timeout when trying to connect to GKE cluster.

## Cause

**Master Authorized Networks** is enabled on the cluster, and your current IP address is not in the allowed list.

This is NOT related to:
- Stale kubeconfig
- Cluster being down
- IAM permissions

## Diagnosis

### 1. Check if cluster is running

```bash
gcloud container clusters list --project trim-array-480700-j7
```

If cluster shows `RUNNING`, the issue is likely IP authorization.

### 2. Check master authorized networks config

```bash
gcloud container clusters describe sukko-develop \
  --zone us-central1-a \
  --project trim-array-480700-j7 \
  --format="yaml(masterAuthorizedNetworksConfig)"
```

Example output showing restricted access:
```yaml
masterAuthorizedNetworksConfig:
  enabled: true
  cidrBlocks:
  - cidrBlock: 175.41.169.97/32
    displayName: some-user
```

### 3. Check your current IP

```bash
curl -s ifconfig.me
```

If your IP is not in the `cidrBlocks` list, you're blocked.

## Fix

### Option A: Add your IP to authorized networks (Quick Fix)

```bash
# Get current authorized IPs first
EXISTING_CIDRS=$(gcloud container clusters describe sukko-develop \
  --zone us-central1-a \
  --project trim-array-480700-j7 \
  --format="value(masterAuthorizedNetworksConfig.cidrBlocks[].cidrBlock)" | tr '\n' ',')

# Add your IP
gcloud container clusters update sukko-develop \
  --zone us-central1-a \
  --project trim-array-480700-j7 \
  --enable-master-authorized-networks \
  --master-authorized-networks "${EXISTING_CIDRS}$(curl -s ifconfig.me)/32"
```

Or if you know there's only one existing IP (e.g., `175.41.169.97/32`):

```bash
gcloud container clusters update sukko-develop \
  --zone us-central1-a \
  --project trim-array-480700-j7 \
  --enable-master-authorized-networks \
  --master-authorized-networks "175.41.169.97/32,$(curl -s ifconfig.me)/32"
```

### Option B: Disable master authorized networks entirely

```bash
gcloud container clusters update sukko-develop \
  --zone us-central1-a \
  --project trim-array-480700-j7 \
  --no-enable-master-authorized-networks
```

This allows access from any IP. Suitable for dev clusters.

### After fixing, refresh kubeconfig

```bash
gcloud container clusters get-credentials sukko-develop \
  --zone us-central1-a \
  --project trim-array-480700-j7
```

Then verify:

```bash
kubectl get nodes
```

## Why This Happens

- Your ISP assigned you a new dynamic IP
- You're working from a different location
- VPN changed your egress IP
- Someone manually restricted cluster access

## Notes

- This restriction is NOT in Terraform - it was manually applied
- On cluster recreation, this restriction will be lost
- The IP `175.41.169.97` belongs to an unknown user - keep it unless you know who it is
