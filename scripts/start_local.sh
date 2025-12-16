#!/bin/bash

# Quick start script for local testing
# This script helps start Redis, MySQL, and the test server

set -e

echo "=== Shock Absorber Local Setup ==="
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Check if Redis is running
echo -e "${YELLOW}Checking Redis...${NC}"
if redis-cli ping > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Redis is running${NC}"
else
    echo -e "${YELLOW}Starting Redis...${NC}"
    # Try different methods to start Redis
    if command -v brew > /dev/null; then
        brew services start redis 2>/dev/null || redis-server --daemonize yes
    elif command -v systemctl > /dev/null; then
        sudo systemctl start redis-server
    elif command -v docker > /dev/null; then
        docker run -d -p 6379:6379 --name redis redis:latest 2>/dev/null || docker start redis
    else
        echo -e "${RED}✗ Could not start Redis. Please start it manually.${NC}"
        exit 1
    fi
    sleep 2
    if redis-cli ping > /dev/null 2>&1; then
        echo -e "${GREEN}✓ Redis started successfully${NC}"
    else
        echo -e "${RED}✗ Failed to start Redis${NC}"
        exit 1
    fi
fi

# Check if MySQL is running
echo -e "${YELLOW}Checking MySQL...${NC}"
if mysql -u root -p'password' -e "SELECT 1;" > /dev/null 2>&1 || mysql -u root -e "SELECT 1;" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ MySQL is running${NC}"
else
    echo -e "${YELLOW}Starting MySQL...${NC}"
    # Try different methods to start MySQL
    if command -v brew > /dev/null; then
        brew services start mysql 2>/dev/null
    elif command -v systemctl > /dev/null; then
        sudo systemctl start mysql
    elif command -v docker > /dev/null; then
        docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=password -e MYSQL_DATABASE=testdb --name mysql mysql:latest 2>/dev/null || docker start mysql
    else
        echo -e "${RED}✗ Could not start MySQL. Please start it manually.${NC}"
        exit 1
    fi
    sleep 3
    echo -e "${GREEN}✓ MySQL started (please verify connection)${NC}"
fi

# Create database and table
echo -e "${YELLOW}Setting up database...${NC}"
if [ -f "scripts/setup_payment_db.sql" ]; then
    # Try with password first, then without
    if mysql -u root -ppassword < scripts/setup_payment_db.sql 2>/dev/null || \
       mysql -u root -p < scripts/setup_payment_db.sql 2>/dev/null; then
        echo -e "${GREEN}✓ Database and table created${NC}"
    else
        echo -e "${YELLOW}⚠ Could not create database automatically. Please run:${NC}"
        echo -e "   ${YELLOW}mysql -u root -p < scripts/setup_payment_db.sql${NC}"
    fi
else
    echo -e "${RED}✗ Database setup script not found${NC}"
fi

echo ""
echo -e "${GREEN}=== Services Ready ===${NC}"
echo ""
echo "Redis: localhost:6379"
echo "MySQL: localhost:3306"
echo ""
echo -e "${YELLOW}Starting test server...${NC}"
echo "Server will be available at: http://localhost:8080"
echo ""
echo "Press Ctrl+C to stop the server"
echo ""

# Start the test server
go run cmd/test-server/main.go

