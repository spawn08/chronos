// Package dynamo provides a DynamoDB-backed Storage adapter for Chronos.
//
// It is built on the official AWS SDK for Go v2 (aws-sdk-go-v2). The previous
// implementation hand-rolled DynamoDB's JSON-1.0 REST protocol over net/http
// and did NOT sign requests with AWS Signature Version 4 (it merely set a bogus
// X-Amz-Access-Key header), so every call against real DynamoDB was rejected.
// Half of its methods were also stubs returning empty slices. This rewrite
// delegates signing, retries, endpoint resolution and (un)marshaling to the
// SDK.
//
// # Data model (single-table design)
//
//	Primary key : pk (S, HASH) + sk (S, RANGE)   — GetItem by entity id
//	GSI "owner-seq-index" : owner (S, HASH) + seq (N, RANGE) — list by parent
//
// Each item stores the entity as a JSON blob in the "data" attribute. Items are
// grouped for listing via the owner attribute (e.g. AGENT#<id>, EVENTS#<sid>);
// the seq attribute orders them (creation time in millis, or event/checkpoint
// sequence number). Memory records also carry kind/mkey attributes so that
// GetMemory (by key) and ListMemory (by kind) can filter server-side.
package dynamo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/spawn08/chronos/storage"
)

const (
	itemSK     = "A" // constant range key for the base item
	ownerIndex = "owner-seq-index"
)

// api is the subset of the DynamoDB client used by Store. It allows injecting a
// fake in unit tests without a live AWS endpoint.
type api interface {
	PutItem(ctx context.Context, in *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	GetItem(ctx context.Context, in *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	DeleteItem(ctx context.Context, in *dynamodb.DeleteItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
	Query(ctx context.Context, in *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	CreateTable(ctx context.Context, in *dynamodb.CreateTableInput, opts ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
}

// Store implements storage.Storage using AWS DynamoDB.
type Store struct {
	client    api
	tableName string
}

// New creates a DynamoDB storage adapter using AWS SigV4-signed requests.
//
// If accessKey/secretKey are empty the default AWS credential chain is used
// (env, shared config, IAM role). For local development against DynamoDB Local
// pass endpoint "http://localhost:8000".
func New(endpoint, tableName, region, accessKey, secretKey string) (*Store, error) {
	ctx := context.Background()
	opts := []func(*awsconfig.LoadOptions) error{}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	if accessKey != "" || secretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("dynamo load config: %w", err)
	}
	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &Store{client: client, tableName: tableName}, nil
}

// NewWithClient wraps a preconfigured client (or fake). Used for tests.
func NewWithClient(client api, tableName string) *Store {
	return &Store{client: client, tableName: tableName}
}

// record is the on-disk representation of every Chronos entity.
type record struct {
	PK    string `dynamodbav:"pk"`
	SK    string `dynamodbav:"sk"`
	Owner string `dynamodbav:"owner,omitempty"`
	Seq   int64  `dynamodbav:"seq,omitempty"`
	Kind  string `dynamodbav:"kind,omitempty"`
	MKey  string `dynamodbav:"mkey,omitempty"`
	Data  []byte `dynamodbav:"data"`
}

func sessionPK(id string) string    { return "SESSION#" + id }
func memoryPK(id string) string     { return "MEMORY#" + id }
func auditPK(id string) string      { return "AUDIT#" + id }
func tracePK(id string) string      { return "TRACE#" + id }
func eventPK(id string) string      { return "EVENT#" + id }
func checkpointPK(id string) string { return "CHECKPOINT#" + id }

func sessionOwner(agentID string) string      { return "AGENT#" + agentID }
func memoryOwner(agentID string) string       { return "MEM#" + agentID }
func auditOwner(sessionID string) string      { return "AUDITS#" + sessionID }
func traceOwner(sessionID string) string      { return "TRACES#" + sessionID }
func eventOwner(sessionID string) string      { return "EVENTS#" + sessionID }
func checkpointOwner(sessionID string) string { return "CKPT#" + sessionID }

// newRecord builds a record wrapping entity v as a JSON blob.
func newRecord(pk string, v any) (record, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return record{}, fmt.Errorf("dynamo marshal entity: %w", err)
	}
	return record{PK: pk, SK: itemSK, Data: data}, nil
}

func (s *Store) put(ctx context.Context, rec record) error {
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return fmt.Errorf("dynamo marshal item: %w", err)
	}
	if _, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &s.tableName,
		Item:      item,
	}); err != nil {
		return fmt.Errorf("dynamo put %s: %w", rec.PK, err)
	}
	return nil
}

// getByID fetches the base item for pk and unmarshals its data blob into out.
func (s *Store) getByID(ctx context.Context, pk string, out any) error {
	resp, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &s.tableName,
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pk},
			"sk": &types.AttributeValueMemberS{Value: itemSK},
		},
	})
	if err != nil {
		return fmt.Errorf("dynamo get %s: %w", pk, err)
	}
	if resp.Item == nil {
		return fmt.Errorf("dynamo: item not found: %s", pk)
	}
	var rec record
	if err := attributevalue.UnmarshalMap(resp.Item, &rec); err != nil {
		return fmt.Errorf("dynamo unmarshal %s: %w", pk, err)
	}
	if err := json.Unmarshal(rec.Data, out); err != nil {
		return fmt.Errorf("dynamo decode %s: %w", pk, err)
	}
	return nil
}

func (s *Store) deleteByID(ctx context.Context, pk string) error {
	if _, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &s.tableName,
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: pk},
			"sk": &types.AttributeValueMemberS{Value: itemSK},
		},
	}); err != nil {
		return fmt.Errorf("dynamo delete %s: %w", pk, err)
	}
	return nil
}

// queryOpts configures a GSI query over an owner partition.
type queryOpts struct {
	owner    string
	forward  bool   // ScanIndexForward: true = ascending seq
	afterSeq *int64 // when set, KeyCondition adds seq > afterSeq
	limit    int32  // 0 = unbounded
	filter   string
	names    map[string]string
	values   map[string]types.AttributeValue
}

// queryData runs a GSI query and returns the raw data blobs of matching items.
func (s *Store) queryData(ctx context.Context, opts queryOpts) ([][]byte, error) {
	names := map[string]string{"#o": "owner"}
	values := map[string]types.AttributeValue{":o": &types.AttributeValueMemberS{Value: opts.owner}}
	keyCond := "#o = :o"
	if opts.afterSeq != nil {
		names["#sq"] = "seq"
		values[":after"] = &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", *opts.afterSeq)}
		keyCond += " AND #sq > :after"
	}
	for k, v := range opts.names {
		names[k] = v
	}
	for k, v := range opts.values {
		values[k] = v
	}

	in := &dynamodb.QueryInput{
		TableName:                 &s.tableName,
		IndexName:                 aws.String(ownerIndex),
		KeyConditionExpression:    aws.String(keyCond),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: values,
		ScanIndexForward:          aws.Bool(opts.forward),
	}
	if opts.filter != "" {
		in.FilterExpression = aws.String(opts.filter)
	}
	if opts.limit > 0 {
		in.Limit = aws.Int32(opts.limit)
	}

	var blobs [][]byte
	for {
		resp, err := s.client.Query(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("dynamo query %s: %w", opts.owner, err)
		}
		for _, item := range resp.Items {
			var rec record
			if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
				return nil, fmt.Errorf("dynamo unmarshal query item: %w", err)
			}
			blobs = append(blobs, rec.Data)
		}
		if resp.LastEvaluatedKey == nil || (opts.limit > 0 && int32(len(blobs)) >= opts.limit) {
			break
		}
		in.ExclusiveStartKey = resp.LastEvaluatedKey
	}
	return blobs, nil
}

// applyOffset drops the first offset elements (DynamoDB has no native offset).
func applyOffset[T any](items []T, offset int) []T {
	if offset <= 0 {
		return items
	}
	if offset >= len(items) {
		return items[:0]
	}
	return items[offset:]
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	rec, err := newRecord(sessionPK(sess.ID), sess)
	if err != nil {
		return err
	}
	rec.Owner = sessionOwner(sess.AgentID)
	rec.Seq = sess.CreatedAt.UnixMilli()
	return s.put(ctx, rec)
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	var sess storage.Session
	if err := s.getByID(ctx, sessionPK(id), &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	return s.CreateSession(ctx, sess)
}

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	blobs, err := s.queryData(ctx, queryOpts{
		owner:   sessionOwner(agentID),
		forward: false, // newest first
		limit:   int32(limit + offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.Session, 0, len(blobs))
	for _, b := range blobs {
		var sess storage.Session
		if json.Unmarshal(b, &sess) == nil {
			out = append(out, &sess)
		}
	}
	out = applyOffset(out, offset)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	rec, err := newRecord(memoryPK(m.ID), m)
	if err != nil {
		return err
	}
	rec.Owner = memoryOwner(m.AgentID)
	rec.Seq = m.CreatedAt.UnixMilli()
	rec.Kind = m.Kind
	rec.MKey = m.Key
	return s.put(ctx, rec)
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	blobs, err := s.queryData(ctx, queryOpts{
		owner:   memoryOwner(agentID),
		forward: false,
		limit:   1,
		filter:  "#mk = :mk",
		names:   map[string]string{"#mk": "mkey"},
		values:  map[string]types.AttributeValue{":mk": &types.AttributeValueMemberS{Value: key}},
	})
	if err != nil {
		return nil, err
	}
	if len(blobs) == 0 {
		return nil, fmt.Errorf("dynamo: memory not found for agent %q key %q", agentID, key)
	}
	var m storage.MemoryRecord
	if err := json.Unmarshal(blobs[0], &m); err != nil {
		return nil, fmt.Errorf("dynamo decode memory: %w", err)
	}
	return &m, nil
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	blobs, err := s.queryData(ctx, queryOpts{
		owner:   memoryOwner(agentID),
		forward: true,
		filter:  "#k = :k",
		names:   map[string]string{"#k": "kind"},
		values:  map[string]types.AttributeValue{":k": &types.AttributeValueMemberS{Value: kind}},
	})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.MemoryRecord, 0, len(blobs))
	for _, b := range blobs {
		var m storage.MemoryRecord
		if json.Unmarshal(b, &m) == nil {
			out = append(out, &m)
		}
	}
	return out, nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	return s.deleteByID(ctx, memoryPK(id))
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	rec, err := newRecord(auditPK(log.ID), log)
	if err != nil {
		return err
	}
	rec.Owner = auditOwner(log.SessionID)
	rec.Seq = log.CreatedAt.UnixMilli()
	return s.put(ctx, rec)
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	blobs, err := s.queryData(ctx, queryOpts{
		owner:   auditOwner(sessionID),
		forward: false,
		limit:   int32(limit + offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.AuditLog, 0, len(blobs))
	for _, b := range blobs {
		var log storage.AuditLog
		if json.Unmarshal(b, &log) == nil {
			out = append(out, &log)
		}
	}
	out = applyOffset(out, offset)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	rec, err := newRecord(tracePK(t.ID), t)
	if err != nil {
		return err
	}
	rec.Owner = traceOwner(t.SessionID)
	rec.Seq = t.StartedAt.UnixMilli()
	return s.put(ctx, rec)
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	var t storage.Trace
	if err := s.getByID(ctx, tracePK(id), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	blobs, err := s.queryData(ctx, queryOpts{owner: traceOwner(sessionID), forward: true})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.Trace, 0, len(blobs))
	for _, b := range blobs {
		var t storage.Trace
		if json.Unmarshal(b, &t) == nil {
			out = append(out, &t)
		}
	}
	return out, nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	rec, err := newRecord(eventPK(e.ID), e)
	if err != nil {
		return err
	}
	rec.Owner = eventOwner(e.SessionID)
	rec.Seq = e.SeqNum
	return s.put(ctx, rec)
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	after := afterSeq
	blobs, err := s.queryData(ctx, queryOpts{
		owner:    eventOwner(sessionID),
		forward:  true,
		afterSeq: &after,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.Event, 0, len(blobs))
	for _, b := range blobs {
		var e storage.Event
		if json.Unmarshal(b, &e) == nil {
			out = append(out, &e)
		}
	}
	return out, nil
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	rec, err := newRecord(checkpointPK(cp.ID), cp)
	if err != nil {
		return err
	}
	rec.Owner = checkpointOwner(cp.SessionID)
	rec.Seq = cp.SeqNum
	return s.put(ctx, rec)
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	var cp storage.Checkpoint
	if err := s.getByID(ctx, checkpointPK(id), &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	blobs, err := s.queryData(ctx, queryOpts{
		owner:   checkpointOwner(sessionID),
		forward: false,
		limit:   1,
	})
	if err != nil {
		return nil, err
	}
	if len(blobs) == 0 {
		return nil, fmt.Errorf("dynamo: no checkpoint found for session %q", sessionID)
	}
	var cp storage.Checkpoint
	if err := json.Unmarshal(blobs[0], &cp); err != nil {
		return nil, fmt.Errorf("dynamo decode checkpoint: %w", err)
	}
	return &cp, nil
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	blobs, err := s.queryData(ctx, queryOpts{owner: checkpointOwner(sessionID), forward: true})
	if err != nil {
		return nil, err
	}
	out := make([]*storage.Checkpoint, 0, len(blobs))
	for _, b := range blobs {
		var cp storage.Checkpoint
		if json.Unmarshal(b, &cp) == nil {
			out = append(out, &cp)
		}
	}
	return out, nil
}

// --- Lifecycle ---

// Migrate creates the single Chronos table with the owner-seq GSI if it does
// not already exist.
func (s *Store) Migrate(ctx context.Context) error {
	_, err := s.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   &s.tableName,
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("pk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("sk"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("owner"), AttributeType: types.ScalarAttributeTypeS},
			{AttributeName: aws.String("seq"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("pk"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("sk"), KeyType: types.KeyTypeRange},
		},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			{
				IndexName: aws.String(ownerIndex),
				KeySchema: []types.KeySchemaElement{
					{AttributeName: aws.String("owner"), KeyType: types.KeyTypeHash},
					{AttributeName: aws.String("seq"), KeyType: types.KeyTypeRange},
				},
				Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
			},
		},
	})
	if err != nil {
		var inUse *types.ResourceInUseException
		if errors.As(err, &inUse) {
			return nil // table already exists
		}
		return fmt.Errorf("dynamo create table: %w", err)
	}
	return nil
}

func (s *Store) Close() error { return nil }

// Ensure Store implements storage.Storage at compile time.
var _ storage.Storage = (*Store)(nil)
