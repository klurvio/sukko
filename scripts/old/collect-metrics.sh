#!/bin/bash

# Metrics Collection Script for Odin WebSocket Servers
# Collects metrics from both Node.js and Go servers

set -e

NODE_PORT=${NODE_PORT:-3001}
GO_PORT=${GO_PORT:-3002}
METRICS_DIR=${METRICS_DIR:-metrics}
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

echo "📊 Odin WebSocket Metrics Collection"
echo "===================================="
echo "Timestamp: $(date)"
echo "Node.js Port: $NODE_PORT"
echo "Go Port: $GO_PORT"
echo "Output Directory: $METRICS_DIR"
echo ""

# Create metrics directory
mkdir -p "$METRICS_DIR"

# Collect Node.js metrics
echo "🟢 Collecting Node.js metrics..."
if curl -s "http://localhost:$NODE_PORT/health" > /dev/null; then
    echo "  ✅ Node.js server is responding"

    # Current metrics
    curl -s "http://localhost:$NODE_PORT/api/metrics" > "$METRICS_DIR/node_metrics_$TIMESTAMP.json"
    echo "  📊 Metrics: $METRICS_DIR/node_metrics_$TIMESTAMP.json"

    # Summary metrics
    curl -s "http://localhost:$NODE_PORT/api/metrics/summary" > "$METRICS_DIR/node_summary_$TIMESTAMP.json"
    echo "  📋 Summary: $METRICS_DIR/node_summary_$TIMESTAMP.json"

    # Health check
    curl -s "http://localhost:$NODE_PORT/health-check" > "$METRICS_DIR/node_health_$TIMESTAMP.json"
    echo "  🩺 Health: $METRICS_DIR/node_health_$TIMESTAMP.json"

    # Error metrics
    curl -s "http://localhost:$NODE_PORT/api/metrics/errors" > "$METRICS_DIR/node_errors_$TIMESTAMP.json"
    echo "  ❌ Errors: $METRICS_DIR/node_errors_$TIMESTAMP.json"

else
    echo "  ❌ Node.js server not responding on port $NODE_PORT"
fi

echo ""

# Collect Go metrics
echo "⚡ Collecting Go metrics..."
if curl -s "http://localhost:$GO_PORT/health" > /dev/null; then
    echo "  ✅ Go server is responding"

    # Stats endpoint
    curl -s "http://localhost:$GO_PORT/stats" > "$METRICS_DIR/go_stats_$TIMESTAMP.json"
    echo "  📊 Stats: $METRICS_DIR/go_stats_$TIMESTAMP.json"

    # Health check
    curl -s "http://localhost:$GO_PORT/health" > "$METRICS_DIR/go_health_$TIMESTAMP.json"
    echo "  🩺 Health: $METRICS_DIR/go_health_$TIMESTAMP.json"

    # Prometheus metrics
    curl -s "http://localhost:$GO_PORT/metrics" > "$METRICS_DIR/go_prometheus_$TIMESTAMP.txt"
    echo "  📈 Prometheus: $METRICS_DIR/go_prometheus_$TIMESTAMP.txt"

else
    echo "  ❌ Go server not responding on port $GO_PORT"
fi

echo ""

# Generate summary
echo "📋 Generating collection summary..."
cat > "$METRICS_DIR/collection_summary_$TIMESTAMP.txt" << EOF
Odin WebSocket Metrics Collection Summary
=========================================
Collection Time: $(date)
Timestamp: $TIMESTAMP

Files Generated:
EOF

# List generated files
find "$METRICS_DIR" -name "*_$TIMESTAMP.*" -type f | while read file; do
    size=$(du -h "$file" | cut -f1)
    echo "  - $(basename "$file") ($size)" >> "$METRICS_DIR/collection_summary_$TIMESTAMP.txt"
done

echo ""
echo "✅ Metrics collection complete!"
echo "📁 Files saved in: $METRICS_DIR/"
echo "📋 Summary: $METRICS_DIR/collection_summary_$TIMESTAMP.txt"
echo ""
echo "💡 Use 'task metrics:generate:report' for live comparison"