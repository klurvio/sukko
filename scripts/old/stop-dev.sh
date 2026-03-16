#!/bin/bash

echo "🛑 Stopping Sukko WebSocket PoC Development Environment"
echo "===================================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to stop process by PID file
stop_process() {
    local name=$1
    local pid_file="logs/${name}.pid"

    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            echo "🔄 Stopping $name (PID: $pid)..."
            kill "$pid"
            sleep 2

            # Force kill if still running
            if ps -p "$pid" > /dev/null 2>&1; then
                echo -e "${YELLOW}⚠️  Force stopping $name...${NC}"
                kill -9 "$pid"
            fi

            echo -e "${GREEN}✅ $name stopped${NC}"
        else
            echo -e "${YELLOW}⚠️  $name process not found${NC}"
        fi
        rm -f "$pid_file"
    else
        echo -e "${YELLOW}⚠️  $name PID file not found${NC}"
    fi
}

# Stop Node.js processes
stop_process "WebSocket Server"
stop_process "Price Publisher"

# Stop Docker services
echo "🐳 Stopping Docker services..."
docker-compose down

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ Docker services stopped${NC}"
else
    echo -e "${YELLOW}⚠️  Some Docker services may still be running${NC}"
fi

# Clean up log directory
if [ -d "logs" ]; then
    echo "🧹 Cleaning up log files..."
    rm -rf logs
    echo -e "${GREEN}✅ Log files cleaned${NC}"
fi

# Kill any remaining Node processes (backup cleanup)
echo "🧹 Cleaning up any remaining processes..."
pkill -f "node src/server.js" > /dev/null 2>&1
pkill -f "node src/publisher.js" > /dev/null 2>&1
pkill -f "nodemon src/server.js" > /dev/null 2>&1

echo ""
echo -e "${GREEN}🎉 Development environment stopped successfully!${NC}"
echo ""
echo "💡 To restart the environment, run: ./scripts/start-dev.sh"