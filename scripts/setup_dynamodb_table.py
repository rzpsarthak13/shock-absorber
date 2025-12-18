#!/usr/bin/env python3
"""
Setup DynamoDB table in LocalStack for Shock Absorber
"""

import sys
import time

try:
    import boto3
    from botocore.exceptions import ClientError
except ImportError:
    print("ERROR: boto3 not installed. Install it with:")
    print("  pip install boto3")
    sys.exit(1)

ENDPOINT = "http://localhost:4566"
REGION = "us-east-1"
TABLE_NAME = "shock-absorber-cache"

def main():
    print("=" * 70)
    print("Setting up DynamoDB table in LocalStack")
    print("=" * 70)
    print(f"Endpoint: {ENDPOINT}")
    print(f"Region: {REGION}")
    print(f"Table: {TABLE_NAME}")
    print()

    # Create DynamoDB client
    dynamodb = boto3.client(
        'dynamodb',
        endpoint_url=ENDPOINT,
        region_name=REGION,
        aws_access_key_id='test',
        aws_secret_access_key='test'
    )

    # Check if table exists
    try:
        response = dynamodb.describe_table(TableName=TABLE_NAME)
        print(f"Table '{TABLE_NAME}' already exists.")
        print("Deleting existing table...")
        dynamodb.delete_table(TableName=TABLE_NAME)
        
        # Wait for deletion
        waiter = dynamodb.get_waiter('table_not_exists')
        waiter.wait(TableName=TABLE_NAME)
        print("✓ Table deleted")
        time.sleep(2)
    except ClientError as e:
        if e.response['Error']['Code'] == 'ResourceNotFoundException':
            print(f"Table '{TABLE_NAME}' does not exist. Creating...")
        else:
            print(f"Error checking table: {e}")
            sys.exit(1)

    # Create table
    try:
        print(f"Creating table '{TABLE_NAME}'...")
        response = dynamodb.create_table(
            TableName=TABLE_NAME,
            AttributeDefinitions=[
                {
                    'AttributeName': 'key',
                    'AttributeType': 'S'
                }
            ],
            KeySchema=[
                {
                    'AttributeName': 'key',
                    'KeyType': 'HASH'
                }
            ],
            BillingMode='PAY_PER_REQUEST'
        )
        
        print("✓ Table creation initiated")
        
        # Wait for table to be active
        print("Waiting for table to be active...")
        waiter = dynamodb.get_waiter('table_exists')
        waiter.wait(TableName=TABLE_NAME)
        
        print("✓ Table is active and ready!")
        
    except ClientError as e:
        print(f"ERROR: Failed to create table: {e}")
        sys.exit(1)

    # Verify table
    try:
        response = dynamodb.describe_table(TableName=TABLE_NAME)
        table_status = response['Table']['TableStatus']
        print()
        print("=" * 70)
        print("Table Setup Complete")
        print("=" * 70)
        print(f"Table Name: {TABLE_NAME}")
        print(f"Status: {table_status}")
        print(f"Endpoint: {ENDPOINT}")
        print(f"Region: {REGION}")
        print()
        print("Next: Update cmd/test-server/main.go to use DynamoDB")
        print("=" * 70)
        
    except ClientError as e:
        print(f"ERROR: Failed to verify table: {e}")
        sys.exit(1)

if __name__ == "__main__":
    main()

