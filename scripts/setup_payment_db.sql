-- Setup script for payment database
-- Run this to create the test database and payment table

CREATE DATABASE IF NOT EXISTS testdb;
USE testdb;

-- Create payment table for testing
CREATE TABLE IF NOT EXISTS payment (
    id VARCHAR(255) PRIMARY KEY,
    amount BIGINT NOT NULL,
    currency VARCHAR(10) NOT NULL,
    upi_transaction_id VARCHAR(255) NOT NULL,
    upi_reference_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255) NOT NULL,
    customer_id VARCHAR(255) NOT NULL,
    leg VARCHAR(50) NOT NULL,
    action VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    error_code VARCHAR(255),
    error_message TEXT,
    payer JSON NOT NULL,
    payees JSON NOT NULL,
    meta JSON NOT NULL,
    resp_meta JSON NOT NULL,
    convenience_fee BIGINT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

-- Create index on upi_reference_id for faster lookups
CREATE INDEX idx_upi_reference_id ON payment(upi_reference_id);

-- Create index on upi_transaction_id
CREATE INDEX idx_upi_transaction_id ON payment(upi_transaction_id);

-- Create index on tenant_id
CREATE INDEX idx_tenant_id ON payment(tenant_id);

-- Create index on customer_id
CREATE INDEX idx_customer_id ON payment(customer_id);
