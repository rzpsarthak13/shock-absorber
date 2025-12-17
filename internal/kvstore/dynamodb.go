package kvstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/rzpsarthak13/shock-absorber/internal/core"
	"github.com/rzpsarthak13/shock-absorber/internal/registry"
)

// DynamoDBKVStore implements the core.KVStore interface using AWS DynamoDB.
type DynamoDBKVStore struct {
	client    *dynamodb.Client
	tableName string
	closed    bool
}

// DynamoDBItem represents an item stored in DynamoDB.
type DynamoDBItem struct {
	Key       string    `dynamodbav:"key"`
	Value     []byte    `dynamodbav:"value"`
	TTL       *int64    `dynamodbav:"ttl,omitempty"`
	CreatedAt time.Time `dynamodbav:"created_at"`
}

// NewDynamoDBKVStore creates a new DynamoDB KV store implementation.
func NewDynamoDBKVStore(region, tableName, endpoint, accessKeyID, secretAccessKey string) (*DynamoDBKVStore, error) {
	if region == "" {
		return nil, fmt.Errorf("region is required")
	}
	if tableName == "" {
		return nil, fmt.Errorf("table name is required")
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Override credentials if provided
	if accessKeyID != "" && secretAccessKey != "" {
		cfg.Credentials = credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")
	}

	// Create DynamoDB client
	clientOptions := []func(*dynamodb.Options){}
	if endpoint != "" {
		// Custom endpoint (e.g., for LocalStack)
		clientOptions = append(clientOptions, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	client := dynamodb.NewFromConfig(cfg, clientOptions...)

	// Test connection by describing the table
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DynamoDB table %s: %w", tableName, err)
	}

	return &DynamoDBKVStore{
		client:    client,
		tableName: tableName,
		closed:    false,
	}, nil
}

// Get retrieves a value by key from the store.
func (d *DynamoDBKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if d.closed {
		return nil, fmt.Errorf("KV store is closed")
	}

	log.Printf("[DYNAMODB] GET operation - Key: %s", key)

	input := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
		},
	}

	result, err := d.client.GetItem(ctx, input)
	if err != nil {
		log.Printf("[DYNAMODB] ERROR: Failed to get key %s: %v", key, err)
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if result.Item == nil {
		log.Printf("[DYNAMODB] Key not found: %s", key)
		return nil, fmt.Errorf("key not found: %s", key)
	}

	// Check TTL if present
	if ttlAttr, ok := result.Item["ttl"]; ok {
		if ttlMember, ok := ttlAttr.(*types.AttributeValueMemberN); ok {
			var ttl int64
			if _, err := fmt.Sscanf(ttlMember.Value, "%d", &ttl); err == nil {
				if time.Now().Unix() > ttl {
					log.Printf("[DYNAMODB] Key %s has expired (TTL: %d)", key, ttl)
					return nil, fmt.Errorf("key not found: %s", key)
				}
			}
		}
	}

	// Extract value
	valueAttr, ok := result.Item["value"]
	if !ok {
		log.Printf("[DYNAMODB] Key %s found but value attribute missing", key)
		return nil, fmt.Errorf("key not found: %s", key)
	}

	valueMember, ok := valueAttr.(*types.AttributeValueMemberB)
	if !ok {
		log.Printf("[DYNAMODB] Key %s found but value is not binary", key)
		return nil, fmt.Errorf("invalid value format for key %s", key)
	}

	value := valueMember.Value
	log.Printf("[DYNAMODB] Successfully retrieved key %s (value size: %d bytes)", key, len(value))

	// Log a preview of the value
	var valuePreview map[string]interface{}
	if err := json.Unmarshal(value, &valuePreview); err == nil {
		log.Printf("[DYNAMODB] Value Preview: %+v", valuePreview)
	}

	return value, nil
}

// Set stores a key-value pair with an optional TTL.
func (d *DynamoDBKVStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if d.closed {
		return fmt.Errorf("KV store is closed")
	}

	log.Printf("[DYNAMODB] SET operation - Key: %s, Value Size: %d bytes, TTL: %v", key, len(value), ttl)

	// Log a preview of the value
	valuePreview := string(value)
	if len(valuePreview) > 200 {
		valuePreview = valuePreview[:200] + "..."
	}
	log.Printf("[DYNAMODB] Value Preview: %s", valuePreview)

	// Also log as JSON if possible
	var jsonPreview map[string]interface{}
	if err := json.Unmarshal(value, &jsonPreview); err == nil {
		log.Printf("[DYNAMODB] Value as JSON: %+v", jsonPreview)
	}

	item := map[string]types.AttributeValue{
		"key":        &types.AttributeValueMemberS{Value: key},
		"value":      &types.AttributeValueMemberB{Value: value},
		"created_at": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
	}

	// Add TTL if specified
	if ttl > 0 {
		ttlTimestamp := time.Now().Add(ttl).Unix()
		item["ttl"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", ttlTimestamp)}
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(d.tableName),
		Item:      item,
	}

	_, err := d.client.PutItem(ctx, input)
	if err != nil {
		log.Printf("[DYNAMODB] ERROR: Failed to set key %s: %v", key, err)
		return fmt.Errorf("failed to set key %s: %w", key, err)
	}

	if ttl > 0 {
		log.Printf("[DYNAMODB] Successfully stored key %s in DynamoDB with TTL %v", key, ttl)
	} else {
		log.Printf("[DYNAMODB] Successfully stored key %s in DynamoDB (no expiration)", key)
	}

	return nil
}

// Delete removes a key from the store.
func (d *DynamoDBKVStore) Delete(ctx context.Context, key string) error {
	if d.closed {
		return fmt.Errorf("KV store is closed")
	}

	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
		},
	}

	_, err := d.client.DeleteItem(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

// Exists checks if a key exists in the store.
func (d *DynamoDBKVStore) Exists(ctx context.Context, key string) (bool, error) {
	if d.closed {
		return false, fmt.Errorf("KV store is closed")
	}

	input := &dynamodb.GetItemInput{
		TableName: aws.String(d.tableName),
		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
		},
		ProjectionExpression: aws.String("key"),
	}

	result, err := d.client.GetItem(ctx, input)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of key %s: %w", key, err)
	}

	if result.Item == nil {
		return false, nil
	}

	// Check TTL if present
	if ttlAttr, ok := result.Item["ttl"]; ok {
		if ttlMember, ok := ttlAttr.(*types.AttributeValueMemberN); ok {
			var ttl int64
			if _, err := fmt.Sscanf(ttlMember.Value, "%d", &ttl); err == nil {
				if time.Now().Unix() > ttl {
					return false, nil
				}
			}
		}
	}

	return true, nil
}

// BatchSet stores multiple key-value pairs atomically with a shared TTL.
// Note: DynamoDB doesn't support true atomic batch writes across items,
// but we use BatchWriteItem for efficiency.
func (d *DynamoDBKVStore) BatchSet(ctx context.Context, items map[string][]byte, ttl time.Duration) error {
	if d.closed {
		return fmt.Errorf("KV store is closed")
	}

	if len(items) == 0 {
		return nil
	}

	// DynamoDB BatchWriteItem can handle up to 25 items per request
	const maxBatchSize = 25
	allItems := make([]map[string]types.AttributeValue, 0, len(items))

	for key, value := range items {
		item := map[string]types.AttributeValue{
			"key":        &types.AttributeValueMemberS{Value: key},
			"value":      &types.AttributeValueMemberB{Value: value},
			"created_at": &types.AttributeValueMemberS{Value: time.Now().UTC().Format(time.RFC3339)},
		}

		// Add TTL if specified
		if ttl > 0 {
			ttlTimestamp := time.Now().Add(ttl).Unix()
			item["ttl"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", ttlTimestamp)}
		}

		allItems = append(allItems, item)
	}

	// Process in batches of 25
	for i := 0; i < len(allItems); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(allItems) {
			end = len(allItems)
		}

		batch := allItems[i:end]
		writeRequests := make([]types.WriteRequest, 0, len(batch))

		for _, item := range batch {
			writeRequests = append(writeRequests, types.WriteRequest{
				PutRequest: &types.PutRequest{
					Item: item,
				},
			})
		}

		input := &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				d.tableName: writeRequests,
			},
		}

		_, err := d.client.BatchWriteItem(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to batch set keys: %w", err)
		}
	}

	return nil
}

// Close closes the connection to the KV store.
func (d *DynamoDBKVStore) Close() error {
	if d.closed {
		return nil
	}

	d.closed = true
	// DynamoDB client doesn't need explicit closing, but we mark it as closed
	return nil
}

// DynamoDBKVStoreFactory implements the KVStoreFactory interface for DynamoDB.
// This factory creates DynamoDB KV store instances using the Strategy pattern.
type DynamoDBKVStoreFactory struct{}

// Type returns the type identifier for this factory.
func (f *DynamoDBKVStoreFactory) Type() string {
	return "dynamodb"
}

// Validate validates the DynamoDB-specific configuration.
func (f *DynamoDBKVStoreFactory) Validate(config KVStoreConfig) error {
	if config.Type != "dynamodb" {
		return fmt.Errorf("invalid type for DynamoDB factory: %s", config.Type)
	}
	if config.Region == "" {
		return fmt.Errorf("region is required for DynamoDB")
	}
	if config.TableName == "" {
		return fmt.Errorf("table_name is required for DynamoDB")
	}
	return nil
}

// Create creates a new DynamoDB KV store instance based on the provided configuration.
func (f *DynamoDBKVStoreFactory) Create(config KVStoreConfig) (core.KVStore, error) {
	// Create DynamoDB KV store using the constructor
	dynamoStore, err := NewDynamoDBKVStore(
		config.Region,
		config.TableName,
		config.Endpoint,
		config.AccessKeyID,
		config.SecretAccessKey,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DynamoDB KV store: %w", err)
	}

	return dynamoStore, nil
}

// DynamoDBConfigValidator implements the ConfigValidator interface for DynamoDB.
// This validator validates DynamoDB-specific configuration using the Strategy pattern.
type DynamoDBConfigValidator struct{}

// Type returns the type identifier for this validator.
func (v *DynamoDBConfigValidator) Type() string {
	return "dynamodb"
}

// Validate validates the DynamoDB-specific configuration in the internal config.
func (v *DynamoDBConfigValidator) Validate(config *registry.InternalConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	kvConfig := config.KVStore

	// Validate type
	if kvConfig.Type != "dynamodb" {
		return fmt.Errorf("invalid type for DynamoDB validator: %s", kvConfig.Type)
	}

	// Validate DynamoDB-specific config
	dynamoConfig := kvConfig.DynamoDBConfig
	if dynamoConfig.Region == "" {
		return fmt.Errorf("region is required for DynamoDB")
	}
	if dynamoConfig.TableName == "" {
		return fmt.Errorf("table_name is required for DynamoDB")
	}

	// Validate common timeouts
	if kvConfig.DialTimeout <= 0 {
		return fmt.Errorf("dial_timeout must be greater than 0, got: %v", kvConfig.DialTimeout)
	}
	if kvConfig.ReadTimeout <= 0 {
		return fmt.Errorf("read_timeout must be greater than 0, got: %v", kvConfig.ReadTimeout)
	}
	if kvConfig.WriteTimeout <= 0 {
		return fmt.Errorf("write_timeout must be greater than 0, got: %v", kvConfig.WriteTimeout)
	}

	// Validate max retries
	if kvConfig.MaxRetries < 0 {
		return fmt.Errorf("max_retries must be non-negative, got: %d", kvConfig.MaxRetries)
	}

	return nil
}

// init auto-registers the DynamoDB factory and validator on package initialization.
// This enables the Strategy pattern - no manual registration needed.
func init() {
	// Register the factory (for KVStoreConfig validation and creation)
	factory := &DynamoDBKVStoreFactory{}
	RegisterFactory(factory)

	// Register the validator (for InternalConfig validation)
	validator := &DynamoDBConfigValidator{}
	registry.RegisterValidator(validator)
}

