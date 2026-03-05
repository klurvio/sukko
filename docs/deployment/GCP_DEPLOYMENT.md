# Google Cloud Platform Deployment Guide

Deploy to single GCE VM using Docker Compose. Start simple, scale later.

---

## 🚀 TL;DR - Automated Deployment

The deployment process is now **fully automated** via Taskfile commands:

```bash
# Complete deployment workflow
task gcp:full-deploy

# Or step-by-step:
task gcp:setup           # Configure GCP CLI
task gcp:enable-apis     # Enable required APIs
task gcp:firewall        # Create firewall rules
task gcp:create-vm       # Create VM instance
task gcp:reserve-ip      # Reserve static IP (optional)
task gcp:deploy:initial  # First-time deployment (interactive)
task gcp:systemd         # Setup auto-start service
```

**See [taskfiles/README.md](../../taskfiles/README.md) for all GCP tasks.**

---

## 🛠️ Production Operations Quick Reference

Common commands for managing your production deployment:

```bash
# Check deployment status and health
task gcp:status              # Show VM status
task gcp:health              # Check application health
task gcp:ip                  # Get external IP address

# View logs and debugging
task gcp:logs                # View application logs
task gcp:logs:tail           # View last 100 lines
task gcp:ssh                 # SSH into GCP instance

# Docker container management
task gcp:docker:ps           # Show running containers
task gcp:docker:up           # Start all services
task gcp:docker:down         # Stop all services
task gcp:docker:restart      # Restart all services

# Service management
task gcp:restart             # Restart services via systemd
task gcp:deploy:update       # Update to latest code

# Publisher control
task gcp:publisher:start              # Start publisher (default 10 msgs/sec)
task gcp:publisher:start RATE=20      # Start with specific rate
task gcp:publisher:stop               # Stop publisher
task gcp:publisher:configure RATE=15  # Change message rate
task gcp:publisher:stats              # View publisher statistics

# VM lifecycle (cost management)
task gcp:stop                # Stop VM (save money when not in use)
task gcp:start               # Start stopped VM

# Maintenance
task gcp:backup              # Create Prometheus/Grafana backups
task gcp:quick-check         # Quick health and status check
```

**Service URLs (replace `$EXTERNAL_IP` with your IP from `task gcp:ip`):**
- WebSocket: `ws://$EXTERNAL_IP:3004/ws`
- Health Check: `http://$EXTERNAL_IP:3004/health`
- Publisher API: `http://$EXTERNAL_IP:3003/control` (use tasks for control)
- Publisher Stats: `http://$EXTERNAL_IP:3003/stats`
- Grafana Dashboard: `http://$EXTERNAL_IP:3010` (admin/admin)
- Prometheus: `http://$EXTERNAL_IP:9091`

---

## Quick Start

**Phase 1 (Now):** Single VM, IP address only, no SSL (Automated via tasks)
**Phase 2 (Later):** Add domain + SSL
**Phase 3 (Future):** Add load balancer for scaling

---

## Phase 1: Deploy to Single VM

> **Note:** All commands below are available as automated tasks. See the TL;DR section above.

### Prerequisites

```bash
# Install gcloud CLI (macOS - already installed ✓)
# Verify installation
gcloud --version

# Authenticate
gcloud auth login

# Create new project (or use existing)
export PROJECT_ID="sukko-server"
gcloud config set project $PROJECT_ID

# Set default region/zone
export REGION=us-central1
export ZONE=us-central1-a
gcloud config set compute/region $REGION
gcloud config set compute/zone $ZONE
```

**OR** use automated task: `task gcp:setup`

### Step 1: Enable APIs

**Automated:** `task gcp:enable-apis`

**Manual:**
```bash
# Enable required APIs
gcloud services enable compute.googleapis.com
# gcloud services enable logging.googleapis.com
# gcloud services enable monitoring.googleapis.com

# Verify
gcloud services list --enabled
```

**OR** use automated task: `task gcp:enable-apis`

### Step 2: Create Firewall Rules

**Automated:** `task gcp:firewall`

**Manual:**
```bash
# Allow SSH (default - already exists)
gcloud compute firewall-rules list | grep default-allow-ssh

# Allow HTTP (port 3004 for WebSocket - no SSL yet)
gcloud compute firewall-rules create allow-websocket \
  --allow tcp:3004 \
  --source-ranges 0.0.0.0/0 \
  --target-tags websocket-server \
  --description "Allow WebSocket connections"

# Allow Grafana (port 3010 - optional, for monitoring)
gcloud compute firewall-rules create allow-grafana \
  --allow tcp:3010 \
  --source-ranges 0.0.0.0/0 \
  --target-tags websocket-server \
  --description "Allow Grafana access"

# Verify
gcloud compute firewall-rules list --filter="name~'allow-'"
```

**OR** use automated task: `task gcp:firewall`

### Step 3: Create VM Instance

**Automated:** `task gcp:create-vm`

**Manual:**
```bash
# Create VM 
# Note $ZONE has to be defined
gcloud compute instances create sukko-server \
  --zone=$ZONE \
  --machine-type=e2-medium \
  --image-family=ubuntu-2404-lts-amd64 \
  --image-project=ubuntu-os-cloud \
  --boot-disk-size=30GB \
  --boot-disk-type=pd-balanced \
  --tags=websocket-server \
  --metadata=startup-script='#!/bin/bash
    apt-get update
    apt-get install -y apt-transport-https ca-certificates curl software-properties-common
    curl -fsSL https://get.docker.com -o get-docker.sh
    sh get-docker.sh
    apt-get install -y docker-compose-plugin git
    curl -sL https://taskfile.dev/install.sh | sh -s -- -d -b /usr/local/bin
    useradd -m -s /bin/bash -G docker deploy
    echo "Startup completed" > /var/log/startup-complete.log'

# Wait for instance to be ready (2-3 minutes)
echo "Waiting for instance to start..."
sleep 120

# Get external IP
export EXTERNAL_IP=$(gcloud compute instances describe sukko-server \
  --zone=$ZONE \
  --format="get(networkInterfaces[0].accessConfigs[0].natIP)")

echo "===================="
echo "VM Created!"
echo "External IP: $EXTERNAL_IP"
echo "===================="
echo ""
echo "Save this IP - you'll need it to connect"
echo "WebSocket URL: ws://$EXTERNAL_IP:3004/ws"
echo "Grafana URL: http://$EXTERNAL_IP:3010"
```

**OR** use automated task: `task gcp:create-vm`

### Step 4: Reserve Static IP (Optional but Recommended)

**Automated:** `task gcp:reserve-ip`

**Manual:**
```bash
# Create static IP
# Note $REGION has to be defined
gcloud compute addresses create sukko-static-ip --region=$REGION

# Get the IP address
STATIC_IP=$(gcloud compute addresses describe sukko-static-ip \
  --region=$REGION \
  --format="get(address)")

echo "Static IP: $STATIC_IP"

# Assign to instance
gcloud compute instances delete-access-config sukko-server \
  --access-config-name="external-nat" \
  --zone=$ZONE

gcloud compute instances add-access-config sukko-server \
  --access-config-name="external-nat" \
  --address=$STATIC_IP \
  --zone=$ZONE

# Update EXTERNAL_IP
export EXTERNAL_IP=$STATIC_IP

echo "Static IP assigned: $EXTERNAL_IP"
```

**OR** use automated task: `task gcp:reserve-ip`

### Step 5: SSH and Setup Application

**Automated:** `task gcp:deploy:initial` (follow interactive prompts)

**Manual:**
```bash
# SSH into instance
gcloud compute ssh sukko-server --zone=$ZONE

# Switch to deploy user
sudo su - deploy

# Clone repository
git clone https://github.com/yourorg/sukko.git
cd sukko

# Create production environment file
cat > .env.production << 'EOF'
NATS_URL=nats://nats:4222
TOKENS=BTC,ETH,SOL,DOGE,SUKKO
PORT=3003
NODE_ENV=production
GF_SECURITY_ADMIN_PASSWORD=CHANGE_THIS_STRONG_PASSWORD
EOF

chmod 600 .env.production

# Create production docker-compose override
cat > docker-compose.prod.yml << 'EOF'
version: "3.8"

services:
  ws-go:
    restart: always
    ports:
      - "3004:3002"  # Expose to external
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  publisher:
    restart: always
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  nats:
    restart: always
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  prometheus:
    restart: always
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--storage.tsdb.retention.time=30d'
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  grafana:
    restart: always
    ports:
      - "3010:3000"  # Expose to external
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=${GF_SECURITY_ADMIN_PASSWORD}
      - GF_SERVER_ROOT_URL=http://${EXTERNAL_IP}:3010
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  loki:
    restart: always
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"

  promtail:
    restart: always
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
EOF
```

**Note:** For automated deployment, follow instructions from `task gcp:deploy:initial`

### Step 6: Build and Start Services

**From VM after initial setup:**
```bash
# Build Docker images
task build:docker

# Start all services
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# Verify all containers running
docker compose ps

# Expected output: All containers should be "Up"
# NAME                COMMAND                  SERVICE      STATUS
# sukko-go          "./sukko-server"       ws-go        Up
# sukko-nats           "nats-server ..."        nats         Up
# sukko-publisher      "node dist/publisher.js" publisher    Up
# sukko-prometheus     "/bin/prometheus ..."    prometheus   Up
# sukko-grafana        "/run.sh"                grafana      Up
# sukko-loki           "/usr/bin/loki ..."      loki         Up
# sukko-promtail       "/usr/bin/promtail ..."  promtail     Up

# Check logs
docker compose logs -f ws-go
# Press Ctrl+C to exit

# Test health endpoint
curl http://localhost:3004/health | jq '.'
```

### Step 7: Create Systemd Service (Auto-start on Reboot)

**Automated:** `task gcp:systemd`

**Manual:**
```bash
# Exit from deploy user
exit

# Create systemd service
sudo tee /etc/systemd/system/sukko.service > /dev/null << 'EOF'
[Unit]
Description=Sukko WebSocket Server
Requires=docker.service
After=docker.service network-online.target
Wants=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/home/deploy/sukko
ExecStart=/usr/bin/docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
ExecStop=/usr/bin/docker compose -f docker-compose.yml -f docker-compose.prod.yml down
User=deploy
Group=deploy
Environment="EXTERNAL_IP=%EXTERNAL_IP%"

[Install]
WantedBy=multi-user.target
EOF

# Replace placeholder with actual IP
sudo sed -i "s/%EXTERNAL_IP%/$EXTERNAL_IP/" /etc/systemd/system/sukko.service

# Enable service
sudo systemctl daemon-reload
sudo systemctl enable sukko
sudo systemctl start sukko
sudo systemctl status sukko

# Exit SSH
exit
```

### Step 8: Verify Deployment from Local Machine

**Automated:** `task gcp:health`

**Manual:**
```bash
# Test health endpoint
curl http://$EXTERNAL_IP:3004/health | jq '.'

# Expected output:
# {
#   "status": "healthy",
#   "healthy": true,
#   "checks": {
#     "nats": { "status": "connected", "healthy": true },
#     ...
#   }
# }

# Test WebSocket connection
npm install -g wscat  # If not installed
wscat -c ws://$EXTERNAL_IP:3004/ws

# Should connect and receive price updates

# Open Grafana
open http://$EXTERNAL_IP:3010
# Login: admin / CHANGE_THIS_STRONG_PASSWORD
```

---

## Testing WebSocket Connection

### From Browser Console

```javascript
// Open http://$EXTERNAL_IP:3010 (or any HTTP page)
// Open browser console and run:

const ws = new WebSocket('ws://YOUR_IP:3004/ws');

ws.onopen = () => console.log('Connected!');
ws.onmessage = (event) => console.log('Message:', event.data);
ws.onerror = (error) => console.error('Error:', error);
ws.onclose = () => console.log('Disconnected');
```

### Using wscat

```bash
# Connect
wscat -c ws://$EXTERNAL_IP:3004/ws

# Should see price updates:
# < {"type":"price_update","token":"BTC","price":45000.23,...}
# < {"type":"price_update","token":"ETH","price":3200.45,...}
```

### Run Stress Test

```bash
# From your local machine (where you have the code)
cd /Volumes/Dev/Codev/Toniq/sukko

# Update stress test script to use your IP
# Edit scripts/stress-test-high-load.cjs
# Change: const WS_URL = 'ws://YOUR_IP:3004';

# Or pass as environment variable
WS_HOST=$EXTERNAL_IP task test:light

# Monitor in Grafana
open http://$EXTERNAL_IP:3010
```

---

## Monitoring

### View Logs

```bash
# SSH to instance
gcloud compute ssh sukko-server --zone=$ZONE

# View all logs
sudo su - deploy
cd sukko
docker compose logs -f

# View specific service
docker compose logs -f ws-go
docker compose logs -f nats
docker compose logs -f publisher

# View last 100 lines
docker compose logs --tail=100 ws-go
```

### Grafana Dashboards

```bash
# Open Grafana
open http://$EXTERNAL_IP:3010

# Default dashboards:
# 1. WebSocket Metrics Dashboard (Prometheus)
#    - Active connections
#    - Message throughput
#    - CPU/Memory usage
#
# 2. WebSocket Logs Stream (Loki)
#    - Real-time logs
#    - Filter by container
```

### Cloud Monitoring (GCP Console)

```bash
# View VM metrics
echo "https://console.cloud.google.com/compute/instancesDetail/zones/$ZONE/instances/sukko-server?project=$PROJECT_ID&tab=monitoring"

# CPU, memory, disk, network
# Auto-collected by Google Cloud
```

---

## Maintenance

### Update Application

```bash
# SSH to instance
gcloud compute ssh sukko-server --zone=$ZONE

# Switch to deploy user
sudo su - deploy
cd sukko

# Pull latest code
git pull origin main

# Rebuild and restart
docker compose -f docker-compose.yml -f docker-compose.prod.yml build
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# Verify
docker compose ps
curl http://localhost:3004/health | jq '.'
```

### Backup Data

```bash
# SSH to instance
gcloud compute ssh sukko-server --zone=$ZONE

# Create backup directory
sudo mkdir -p /backups

# Backup script
sudo tee /opt/backup-sukko.sh > /dev/null << 'EOF'
#!/bin/bash
BACKUP_DIR=/backups
DATE=$(date +%Y%m%d-%H%M%S)

mkdir -p $BACKUP_DIR

# Backup Prometheus data
docker run --rm \
  -v sukko_prometheus_data:/data \
  -v $BACKUP_DIR:/backup \
  alpine tar czf /backup/prometheus-$DATE.tar.gz /data

# Backup Grafana data
docker run --rm \
  -v sukko_grafana_data:/data \
  -v $BACKUP_DIR:/backup \
  alpine tar czf /backup/grafana-$DATE.tar.gz /data

# Keep last 7 days
find $BACKUP_DIR -name "*.tar.gz" -mtime +7 -delete

echo "Backup completed: $DATE"
EOF

sudo chmod +x /opt/backup-sukko.sh

# Run backup manually
sudo /opt/backup-sukko.sh

# Schedule daily backup (2 AM)
sudo crontab -e
# Add: 0 2 * * * /opt/backup-sukko.sh >> /var/log/sukko-backup.log 2>&1
```

### Restart Services

```bash
# Restart all
gcloud compute ssh sukko-server --zone=$ZONE --command="sudo systemctl restart sukko"

# Or SSH in and restart specific service
gcloud compute ssh sukko-server --zone=$ZONE
sudo su - deploy
cd sukko
docker compose restart ws-go
```

### Check Resource Usage

```bash
# SSH to instance
gcloud compute ssh sukko-server --zone=$ZONE

# Check system resources
htop  # If installed: sudo apt install htop

# Check disk usage
df -h

# Check memory
free -h

# Check Docker resources
docker stats
```

---

## Phase 2: Add Domain + SSL (Later)

When you have a domain:

### Option A: Real Domain

```bash
# 1. Buy domain (e.g., from Cloudflare, Namecheap)
# 2. Add DNS A record:
#    ws.yourdomain.com  A  $EXTERNAL_IP

# 3. SSH to instance
gcloud compute ssh sukko-server --zone=$ZONE

# 4. Install Nginx + Certbot
sudo apt install -y nginx certbot python3-certbot-nginx

# 5. Configure Nginx (see nginx config below)

# 6. Get SSL certificate
sudo certbot --nginx -d ws.yourdomain.com

# 7. Update firewall
gcloud compute firewall-rules create allow-https \
  --allow tcp:443 \
  --source-ranges 0.0.0.0/0 \
  --target-tags websocket-server

# 8. Connect to wss://ws.yourdomain.com/ws
```

### Option B: Free Subdomain (nip.io)

```bash
# Use magic DNS: $EXTERNAL_IP.nip.io
# Example: 35.123.45.67.nip.io resolves to 35.123.45.67

# Install Nginx + Certbot
sudo apt install -y nginx certbot python3-certbot-nginx

# Get SSL for nip.io domain
sudo certbot --nginx -d $EXTERNAL_IP.nip.io

# Connect to: wss://$EXTERNAL_IP.nip.io/ws
```

### Nginx Configuration (for SSL)

```nginx
# /etc/nginx/sites-available/websocket
upstream websocket_backend {
    server 127.0.0.1:3004;
    keepalive 64;
}

# HTTP to HTTPS redirect
server {
    listen 80;
    server_name ws.yourdomain.com;
    return 301 https://$host$request_uri;
}

# HTTPS WebSocket
server {
    listen 443 ssl http2;
    server_name ws.yourdomain.com;

    # SSL certificates (added by Certbot)
    ssl_certificate /etc/letsencrypt/live/ws.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/ws.yourdomain.com/privkey.pem;

    ssl_protocols TLSv1.2 TLSv1.3;

    location /ws {
        proxy_pass http://websocket_backend;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;

        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }

    location /health {
        proxy_pass http://websocket_backend/health;
    }
}
```

---

## Phase 3: Add Load Balancer (Future Scaling)

When you need to scale beyond 1 VM:

```bash
# 1. Create instance template from existing VM
gcloud compute instance-templates create sukko-template \
  --source-instance=sukko-server \
  --source-instance-zone=$ZONE

# 2. Create managed instance group
gcloud compute instance-groups managed create sukko-group \
  --base-instance-name=sukko \
  --template=sukko-template \
  --size=3 \
  --zone=$ZONE

# 3. Create health check
gcloud compute health-checks create http sukko-health \
  --port=3004 \
  --request-path=/health

# 4. Create backend service
gcloud compute backend-services create sukko-backend \
  --protocol=HTTP \
  --health-checks=sukko-health \
  --global \
  --session-affinity=CLIENT_IP

# 5. Add instance group to backend
gcloud compute backend-services add-backend sukko-backend \
  --instance-group=sukko-group \
  --instance-group-zone=$ZONE \
  --global

# 6. Create load balancer (see detailed steps in architecture docs)
```

---

## Taskfile Integration

**✅ Already implemented!** All GCP tasks are available in `taskfiles/deploy-gcp.yml`.

### Available GCP Tasks

```bash
# Infrastructure
task gcp:setup           # Configure gcloud CLI
task gcp:enable-apis     # Enable GCP APIs
task gcp:firewall        # Create firewall rules
task gcp:create-vm       # Create VM instance
task gcp:reserve-ip      # Reserve static IP

# Deployment
task gcp:deploy:initial  # First-time deployment
task gcp:deploy:update   # Update existing deployment
task gcp:systemd         # Setup systemd auto-start

# Operations
task gcp:ssh             # SSH into instance
task gcp:ip              # Get external IP
task gcp:health          # Check deployment health
task gcp:logs            # View application logs
task gcp:logs:tail       # View last 100 lines
task gcp:restart         # Restart services

# VM Lifecycle
task gcp:start           # Start VM
task gcp:stop            # Stop VM (save money)
task gcp:status          # Show VM status
task gcp:delete          # Delete VM (WARNING!)

# Maintenance
task gcp:backup          # Create backups

# Combined Workflows
task gcp:full-deploy     # Complete deployment workflow
task gcp:quick-check     # Quick health and status check
```

### Environment Variable Overrides

```bash
# Custom project/region/zone
export GCP_PROJECT_ID=my-project
export GCP_REGION=us-west1
export GCP_ZONE=us-west1-a
task gcp:create-vm

# Custom machine type
export GCP_MACHINE_TYPE=e2-standard-2
task gcp:create-vm
```

See full implementation in `taskfiles/deploy-gcp.yml`.

---

## CI/CD (Optional)

### GitHub Actions Deployment

```yaml
# .github/workflows/deploy-gcp.yml
name: Deploy to GCP

on:
  push:
    branches: [main]
  workflow_dispatch:

env:
  PROJECT_ID: sukko-server
  ZONE: us-central1-a
  INSTANCE: sukko-server

jobs:
  deploy:
    runs-on: ubuntu-latest

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Authenticate to Google Cloud
        uses: google-github-actions/auth@v2
        with:
          credentials_json: ${{ secrets.GCP_SA_KEY }}

      - name: Set up Cloud SDK
        uses: google-github-actions/setup-gcloud@v2

      - name: Deploy
        run: |
          gcloud compute ssh $INSTANCE \
            --zone=$ZONE \
            --command="sudo su - deploy -c 'cd sukko && \
              git pull origin main && \
              docker compose -f docker-compose.yml -f docker-compose.prod.yml build && \
              docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d'"

      - name: Health Check
        run: |
          sleep 10
          IP=$(gcloud compute instances describe $INSTANCE \
            --zone=$ZONE \
            --format="get(networkInterfaces[0].accessConfigs[0].natIP)")
          curl -f http://$IP:3004/health
```

**Setup:**
1. Create service account in GCP with Compute Admin role
2. Download JSON key
3. Add to GitHub secrets as `GCP_SA_KEY`

---

## Cost Breakdown

### Single e2-medium VM

| Item | Spec | Monthly Cost |
|------|------|--------------|
| e2-medium | 2 vCPU, 4GB RAM | ~$27.40 |
| Static IP (optional) | 1 IP | $3.00 |
| Standard disk | 30GB | $1.20 |
| Egress | First 1GB free | $5-20 |
| **Total** | | **$36-51/month** |

### Cost Optimization

```bash
# Use preemptible/spot instance (60-91% cheaper)
# Warning: Can be terminated at any time, not for production
gcloud compute instances create sukko-server \
  --zone=$ZONE \
  --machine-type=e2-medium \
  --provisioning-model=SPOT \
  --instance-termination-action=STOP
  # Cost: ~$8/month instead of $27

# Or use committed use discount
# 1-year commit: 37% discount
# 3-year commit: 55% discount
```

---

## Troubleshooting

### Can't connect to WebSocket

```bash
# Check firewall
gcloud compute firewall-rules list | grep allow-websocket

# Check if container is running
gcloud compute ssh sukko-server --zone=$ZONE
docker compose ps

# Check logs
docker compose logs ws-go

# Check if port is listening
netstat -tlnp | grep 3004
```

### Container won't start

```bash
# SSH to instance
gcloud compute ssh sukko-server --zone=$ZONE

# Check logs
sudo su - deploy
cd sukko
docker compose logs

# Check disk space
df -h

# Restart all
docker compose -f docker-compose.yml -f docker-compose.prod.yml restart
```

### High CPU/Memory

```bash
# Check resource usage
docker stats

# Check system resources
htop

# Upgrade VM if needed
gcloud compute instances stop sukko-server --zone=$ZONE
gcloud compute instances set-machine-type sukko-server \
  --machine-type=e2-standard-2 \
  --zone=$ZONE
gcloud compute instances start sukko-server --zone=$ZONE
```

---

## Quick Reference

### Essential Commands

```bash
# SSH
gcloud compute ssh sukko-server --zone=us-central1-a

# Get IP
gcloud compute instances describe sukko-server \
  --zone=us-central1-a \
  --format="get(networkInterfaces[0].accessConfigs[0].natIP)"

# View logs
gcloud compute ssh sukko-server --zone=us-central1-a \
  --command="sudo su - deploy -c 'cd sukko && docker compose logs -f'"

# Restart services
gcloud compute ssh sukko-server --zone=us-central1-a \
  --command="sudo systemctl restart sukko"

# Stop VM (save money when not using)
gcloud compute instances stop sukko-server --zone=us-central1-a

# Start VM
gcloud compute instances start sukko-server --zone=us-central1-a
```

### URLs

```bash
# Get your IP first
IP=$(gcloud compute instances describe sukko-server \
  --zone=us-central1-a \
  --format="get(networkInterfaces[0].accessConfigs[0].natIP)")

# Access URLs
echo "WebSocket: ws://$IP:3004/ws"
echo "Health: http://$IP:3004/health"
echo "Grafana: http://$IP:3010"
```

---

## Next Steps

1. ✅ Run deployment commands above
2. ✅ Get your external IP
3. ✅ Test WebSocket connection
4. ✅ Open Grafana and check dashboards
5. ✅ Run stress tests
6. 📅 Later: Add domain + SSL
7. 📅 Future: Add load balancer for scaling

**Deployment time: 15-30 minutes**

Ready to deploy!
