#!/bin/bash

echo "🚀 Starting Odin WebSocket PoC Development Environment"
echo "=================================================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check prerequisites
echo "🔍 Checking prerequisites..."

if ! command_exists docker; then
    echo -e "${RED}❌ Docker is not installed${NC}"
    echo "Please install Docker: https://docs.docker.com/get-docker/"
    exit 1
fi

if ! command_exists node; then
    echo -e "${RED}❌ Node.js is not installed${NC}"
    echo "Please install Node.js: https://nodejs.org/"
    exit 1
fi

if ! command_exists npm; then
    echo -e "${RED}❌ npm is not installed${NC}"
    echo "Please install npm (usually comes with Node.js)"
    exit 1
fi

echo -e "${GREEN}✅ All prerequisites found${NC}"

# Create .env file if it doesn't exist
if [ ! -f ".env" ]; then
    echo "📝 Creating .env file from .env.example..."
    cp .env.example .env
    echo -e "${YELLOW}⚠️  Please review and update .env file if needed${NC}"
fi

# Install dependencies
echo "📦 Installing Node.js dependencies..."
npm install

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to install dependencies${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Dependencies installed${NC}"

# Start NATS server
echo "🐳 Starting NATS server with Docker..."
docker-compose up -d nats

if [ $? -ne 0 ]; then
    echo -e "${RED}❌ Failed to start NATS server${NC}"
    exit 1
fi

# Wait for NATS to be ready
echo "⏳ Waiting for NATS server to be ready..."
sleep 5

# Check NATS health
echo "🏥 Checking NATS server health..."
curl -f http://localhost:8222/healthz > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✅ NATS server is healthy${NC}"
else
    echo -e "${YELLOW}⚠️  NATS server might still be starting up${NC}"
fi

# Function to start component in background
start_component() {
    local name=$1
    local command=$2
    local log_file="logs/${name}.log"

    mkdir -p logs

    echo "🔄 Starting $name..."
    eval "$command" > "$log_file" 2>&1 &
    local pid=$!
    echo $pid > "logs/${name}.pid"
    echo -e "${GREEN}✅ $name started (PID: $pid)${NC}"
    echo "   Log file: $log_file"
}

# Start all components
echo ""
echo "🚀 Starting application components..."

start_component "WebSocket Server" "npm run dev"
sleep 2

start_component "Price Publisher" "npm run publisher"
sleep 1

echo ""
echo -e "${GREEN}🎉 Development environment started successfully!${NC}"
echo ""
echo "📋 Available services:"
echo "   • NATS Server:      http://localhost:8222 (monitoring)"
echo "   • WebSocket Server: ws://localhost:3000/ws"
echo "   • HTTP API:         http://localhost:3000"
echo "   • HTML Client:      file://$(pwd)/client/index.html"
echo ""
echo "🔧 Commands:"
echo "   • View logs:        tail -f logs/websocket-server.log"
echo "   • View publisher:   tail -f logs/price-publisher.log"
echo "   • Run load test:    npm run test"
echo "   • Stop services:    ./scripts/stop-dev.sh"
echo ""
echo "🌐 Open the HTML client:"
echo "   open client/index.html"
echo ""
echo -e "${BLUE}💡 Tip: Check logs/websocket-server.log for server output${NC}"

# Wait a moment and show status
sleep 3
echo ""
echo "📊 Component Status:"

# Check if processes are running
check_process() {
    local name=$1
    local pid_file="logs/${name}.pid"

    if [ -f "$pid_file" ]; then
        local pid=$(cat "$pid_file")
        if ps -p "$pid" > /dev/null 2>&1; then
            echo -e "   ${GREEN}✅ $name (PID: $pid)${NC}"
        else
            echo -e "   ${RED}❌ $name (Process not found)${NC}"
        fi
    else
        echo -e "   ${YELLOW}⚠️  $name (PID file not found)${NC}"
    fi
}

check_process "WebSocket Server"
check_process "Price Publisher"

# Check NATS
curl -f http://localhost:8222/healthz > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo -e "   ${GREEN}✅ NATS Server${NC}"
else
    echo -e "   ${RED}❌ NATS Server${NC}"
fi

echo ""
echo "🎯 Ready to test! Open client/index.html in your browser and click Connect."