#!/bin/bash
# Script to start Redis and MySQL in Docker for Shock Absorber testing

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Starting Redis and MySQL in Docker                     ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo -e "${RED}✗ Docker is not running${NC}"
    echo -e "${YELLOW}Please start Docker Desktop or Docker daemon${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Docker is running${NC}"
echo ""

# Check if docker-compose is available
if command -v docker-compose &> /dev/null; then
    DOCKER_COMPOSE_CMD="docker-compose"
elif docker compose version &> /dev/null; then
    DOCKER_COMPOSE_CMD="docker compose"
else
    echo -e "${RED}✗ docker-compose not found${NC}"
    exit 1
fi

echo -e "${YELLOW}Starting services with docker-compose...${NC}"
echo ""

# Start services
$DOCKER_COMPOSE_CMD up -d

echo ""
echo -e "${YELLOW}Waiting for services to be healthy...${NC}"

# Wait for Redis
echo -n "Waiting for Redis "
for i in {1..30}; do
    if docker exec shock-absorber-redis redis-cli ping > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
done

# Wait for MySQL
echo -n "Waiting for MySQL "
for i in {1..60}; do
    if docker exec shock-absorber-mysql mysqladmin ping -h localhost -u root -ppassword --silent > /dev/null 2>&1; then
        echo -e "${GREEN}✓${NC}"
        break
    fi
    echo -n "."
    sleep 1
done

echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Services Started Successfully                ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Redis:   ${BLUE}localhost:6379${NC}"
echo -e "MySQL:   ${BLUE}localhost:3306${NC}"
echo -e "  User:     ${BLUE}root${NC}"
echo -e "  Password: ${BLUE}password${NC}"
echo -e "  Database: ${BLUE}testdb${NC}"
echo ""
echo -e "${YELLOW}Useful commands:${NC}"
echo -e "  Stop services:    ${BLUE}$DOCKER_COMPOSE_CMD down${NC}"
echo -e "  View logs:         ${BLUE}$DOCKER_COMPOSE_CMD logs -f${NC}"
echo -e "  Stop and remove:   ${BLUE}$DOCKER_COMPOSE_CMD down -v${NC}"
echo ""

