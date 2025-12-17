#!/bin/bash
# Setup DynamoDB table in LocalStack for Shock Absorber testing

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

ENDPOINT="${DYNAMODB_ENDPOINT:-http://localhost:4566}"
REGION="${AWS_REGION:-us-east-1}"
TABLE_NAME="${DYNAMODB_TABLE:-shock-absorber-cache}"

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║     Setting up DynamoDB in LocalStack                    ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""

# Check if LocalStack is running
echo -e "${YELLOW}Checking LocalStack connection...${NC}"
if curl -s "$ENDPOINT/_localstack/health" > /dev/null 2>&1; then
    echo -e "${GREEN}✓ LocalStack is running${NC}"
else
    echo -e "${RED}✗ LocalStack is not running at $ENDPOINT${NC}"
    echo -e "${YELLOW}Starting LocalStack...${NC}"
    
    # Try to start LocalStack
    if docker ps -a | grep -q localstack; then
        docker start localstack
        echo -e "${YELLOW}Waiting for LocalStack to be ready...${NC}"
        sleep 5
    else
        echo -e "${YELLOW}Creating LocalStack container...${NC}"
        docker run -d \
            --name localstack \
            -p 4566:4566 \
            -e SERVICES=dynamodb \
            localstack/localstack:latest
        
        echo -e "${YELLOW}Waiting for LocalStack to be ready...${NC}"
        sleep 10
    fi
    
    # Check again
    if curl -s "$ENDPOINT/_localstack/health" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ LocalStack is now running${NC}"
    else
        echo -e "${RED}✗ Failed to start LocalStack${NC}"
        echo -e "${YELLOW}Please start LocalStack manually:${NC}"
        echo -e "  ${BLUE}docker run -d --name localstack -p 4566:4566 -e SERVICES=dynamodb localstack/localstack:latest${NC}"
        exit 1
    fi
fi

echo ""

# Check if AWS CLI is available
if ! command -v aws &> /dev/null; then
    echo -e "${RED}✗ AWS CLI not found${NC}"
    echo -e "${YELLOW}Please install AWS CLI:${NC}"
    echo -e "  ${BLUE}brew install awscli${NC}  # macOS"
    echo -e "  ${BLUE}pip install awscli${NC}   # Linux"
    exit 1
fi

echo -e "${YELLOW}Creating DynamoDB table: $TABLE_NAME${NC}"
echo -e "  Endpoint: ${BLUE}$ENDPOINT${NC}"
echo -e "  Region: ${BLUE}$REGION${NC}"
echo ""

# Check if table already exists
if aws dynamodb describe-table \
    --table-name "$TABLE_NAME" \
    --endpoint-url "$ENDPOINT" \
    --region "$REGION" \
    --no-cli-pager > /dev/null 2>&1; then
    echo -e "${YELLOW}Table already exists. Deleting and recreating...${NC}"
    aws dynamodb delete-table \
        --table-name "$TABLE_NAME" \
        --endpoint-url "$ENDPOINT" \
        --region "$REGION" \
        --no-cli-pager > /dev/null 2>&1
    
    echo -e "${YELLOW}Waiting for table deletion...${NC}"
    sleep 2
fi

# Create table
aws dynamodb create-table \
    --table-name "$TABLE_NAME" \
    --attribute-definitions \
        AttributeName=key,AttributeType=S \
    --key-schema \
        AttributeName=key,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    --endpoint-url "$ENDPOINT" \
    --region "$REGION" \
    --no-cli-pager > /dev/null 2>&1

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Table created successfully${NC}"
else
    echo -e "${RED}✗ Failed to create table${NC}"
    exit 1
fi

# Wait for table to be active
echo -e "${YELLOW}Waiting for table to be active...${NC}"
sleep 2

# Verify table exists
if aws dynamodb describe-table \
    --table-name "$TABLE_NAME" \
    --endpoint-url "$ENDPOINT" \
    --region "$REGION" \
    --no-cli-pager > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Table is ready${NC}"
else
    echo -e "${RED}✗ Table verification failed${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              DynamoDB Setup Complete                      ║${NC}"
echo -e "${GREEN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Table Name: ${BLUE}$TABLE_NAME${NC}"
echo -e "Endpoint:   ${BLUE}$ENDPOINT${NC}"
echo -e "Region:     ${BLUE}$REGION${NC}"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo -e "1. Update ${BLUE}cmd/test-server/main.go${NC} to use DynamoDB:"
echo -e "   ${BLUE}config.KVStore.Type = \"dynamodb\"${NC}"
echo -e "   ${BLUE}config.KVStore.DynamoDBConfig = shockabsorber.DynamoDBConfig{${NC}"
echo -e "       ${BLUE}Region: \"$REGION\",${NC}"
echo -e "       ${BLUE}TableName: \"$TABLE_NAME\",${NC}"
echo -e "       ${BLUE}Endpoint: \"$ENDPOINT\",${NC}"
echo -e "       ${BLUE}AccessKeyID: \"test\",${NC}"
echo -e "       ${BLUE}SecretAccessKey: \"test\",${NC}"
echo -e "   ${BLUE}}${NC}"
echo ""
echo -e "2. Start test server: ${BLUE}go run cmd/test-server/main.go${NC}"
echo ""

