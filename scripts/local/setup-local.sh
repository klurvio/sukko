#!/bin/bash
# Setup Local Development Environment
# Creates Redpanda topics and starts all services

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "🚀 Setting up Odin WebSocket local development environment..."
echo ""

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Error: Docker is not running"
    echo "   Please start Docker Desktop and try again"
    exit 1
fi

# Check if .env.local exists
if [ ! -f .env.local ]; then
    echo "⚠️  .env.local not found, creating from example..."
    cp .env.local.example .env.local
    echo "   ✅ Created .env.local"
    echo ""
fi

# Start services
echo "📦 Starting all services..."
docker-compose up -d

echo ""
echo "⏳ Waiting for services to be healthy..."

# Wait for Redpanda to be healthy
echo "   Waiting for Redpanda..."
timeout=60
elapsed=0
while [ $elapsed -lt $timeout ]; do
    if docker exec redpanda-local rpk cluster health 2>/dev/null | grep -q "Healthy:.*true"; then
        echo "   ✅ Redpanda is healthy"
        break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
done

if [ $elapsed -ge $timeout ]; then
    echo "   ❌ Redpanda failed to start within ${timeout}s"
    echo "   Check logs: docker-compose logs redpanda"
    exit 1
fi

# Create Redpanda topics
echo ""
echo "📝 Creating Redpanda topics..."
bash ../../scripts/setup-redpanda-topics.sh redpanda-local

echo ""
echo "✅ Local development environment is ready!"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📊 Service URLs:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  🌐 Redpanda Console:  http://localhost:8080"
echo "  📈 Grafana:           http://localhost:3010  (admin/admin)"
echo "  📊 Prometheus:        http://localhost:9091"
echo "  📝 Loki:              http://localhost:3100"
echo "  🔧 Publisher API:     http://localhost:3003"
echo "  🔌 WebSocket Server:  ws://localhost:3004/ws"
echo "  ⚙️  Redpanda Admin:    http://localhost:9644"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎯 Next Steps:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "  1. View logs:    docker-compose logs -f ws-server"
echo "  2. Start events: curl -X POST http://localhost:3003/start \\"
echo "                     -H 'Content-Type: application/json' \\"
echo "                     -d '{\"rate\": 10, \"tokenIds\": [\"BTC\", \"ETH\", \"SOL\"]}'"
echo "  3. Check health: curl http://localhost:3004/health"
echo "  4. View metrics: http://localhost:9091/targets"
echo "  5. Stop all:     docker-compose down"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
