// Package dynamostore provides a DynamoDB implementation of flowstate store interfaces
// using a single-table design with PK/SK patterns.
//
// Table design:
//
//	PK                              SK                          Type
//	EVENT#{aggType}#{aggID}         {eventID}                   Event
//	CORR#{correlationID}            {eventID}                   Event (GSI)
//	INST#{aggType}#{aggID}          INSTANCE                    Instance
//	TASK#{taskID}                   TASK                        Task
//	TASK_AGG#{aggType}#{aggID}      {taskID}                    Task (by aggregate)
//	CHILD#{childAggType}#{childID}  CHILD                       Child relation
//	CHILD_PARENT#{parentType}#{id}  {childID}                   Child (by parent)
//	CHILD_GROUP#{groupID}           {childID}                   Child (by group)
//	ACTIVITY#{invocationID}         ACTIVITY                    Activity
//	ACTIVITY_AGG#{aggType}#{aggID}  {invocationID}              Activity (by aggregate)
package dynamostore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// Store implements flowstate EventStore, InstanceStore, TaskStore,
// ChildStore, and ActivityStore using a single DynamoDB table.
type Store struct {
	client    *dynamodb.Client
	tableName string
	errNotFound error
}

// New creates a new DynamoDB Store.
func New(client *dynamodb.Client, tableName string, errNotFound error) *Store {
	return &Store{
		client:      client,
		tableName:   tableName,
		errNotFound: errNotFound,
	}
}

// TxProvider implements flowstate.TxProvider using DynamoDB TransactWriteItems.
// Since DynamoDB transactions work differently, this collects items and writes them atomically on Commit.
type TxProvider struct {
	client    *dynamodb.Client
	tableName string
}

// NewTxProvider creates a new DynamoDB TxProvider.
func NewTxProvider(client *dynamodb.Client, tableName string) *TxProvider {
	return &TxProvider{client: client, tableName: tableName}
}

type ddbTx struct {
	items []ddbtypes.TransactWriteItem
}

func (p *TxProvider) Begin(_ context.Context) (any, error) {
	return &ddbTx{}, nil
}

func (p *TxProvider) Commit(ctx context.Context, tx any) error {
	t := tx.(*ddbTx)
	if len(t.items) == 0 {
		return nil
	}
	_, err := p.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: t.items,
	})
	if err != nil {
		// DynamoDB evaluates ConditionExpressions at commit time for transactional writes.
		// When an instance Update's condition fails (stale optimistic lock), DynamoDB returns
		// TransactionCanceledException with a ConditionalCheckFailed cancellation reason.
		// Map this to ErrConcurrentModification so callers see the same error as the non-tx path.
		var txCanceled *ddbtypes.TransactionCanceledException
		if errors.As(err, &txCanceled) {
			for _, reason := range txCanceled.CancellationReasons {
				if reason.Code != nil && *reason.Code == "ConditionalCheckFailed" {
					return fmt.Errorf("dynamostore: commit tx: %w", flowstate.ErrConcurrentModification)
				}
			}
		}
		return fmt.Errorf("dynamostore: commit tx: %w", err)
	}
	return nil
}

func (p *TxProvider) Rollback(_ context.Context, _ any) error {
	return nil // No-op: items are discarded
}

// --- EventStore ---

func (s *Store) Append(ctx context.Context, tx any, event types.DomainEvent) error {
	payload, _ := json.Marshal(event.Payload)
	stateBefore, _ := json.Marshal(event.StateBefore)
	stateAfter, _ := json.Marshal(event.StateAfter)

	item := map[string]ddbtypes.AttributeValue{
		"PK":              &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("EVENT#%s#%s", event.AggregateType, event.AggregateID)},
		"SK":              &ddbtypes.AttributeValueMemberS{Value: event.ID},
		"GSI1PK":          &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("CORR#%s", event.CorrelationID)},
		"GSI1SK":          &ddbtypes.AttributeValueMemberS{Value: event.ID},
		"Type":            &ddbtypes.AttributeValueMemberS{Value: "Event"},
		"ID":              &ddbtypes.AttributeValueMemberS{Value: event.ID},
		"AggregateType":   &ddbtypes.AttributeValueMemberS{Value: event.AggregateType},
		"AggregateID":     &ddbtypes.AttributeValueMemberS{Value: event.AggregateID},
		"WorkflowType":    &ddbtypes.AttributeValueMemberS{Value: event.WorkflowType},
		"WorkflowVersion": &ddbtypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", event.WorkflowVersion)},
		"EventType":       &ddbtypes.AttributeValueMemberS{Value: event.EventType},
		"CorrelationID":   &ddbtypes.AttributeValueMemberS{Value: event.CorrelationID},
		"ActorID":         &ddbtypes.AttributeValueMemberS{Value: event.ActorID},
		"TransitionName":  &ddbtypes.AttributeValueMemberS{Value: event.TransitionName},
		"StateBefore":     &ddbtypes.AttributeValueMemberS{Value: string(stateBefore)},
		"StateAfter":      &ddbtypes.AttributeValueMemberS{Value: string(stateAfter)},
		"Payload":         &ddbtypes.AttributeValueMemberS{Value: string(payload)},
		"CreatedAt":       &ddbtypes.AttributeValueMemberS{Value: event.CreatedAt.Format(time.RFC3339Nano)},
	}

	if tx != nil {
		t := tx.(*ddbTx)
		t.items = append(t.items, ddbtypes.TransactWriteItem{
			Put: &ddbtypes.Put{
				TableName: aws.String(s.tableName),
				Item:      item,
			},
		})
		return nil
	}

	_, err := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	return err
}

func (s *Store) ListByCorrelation(ctx context.Context, correlationID string) ([]types.DomainEvent, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("GSI1"),
		KeyConditionExpression: aws.String("GSI1PK = :pk"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":pk": &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("CORR#%s", correlationID)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamostore: query by correlation: %w", err)
	}
	return unmarshalEvents(out.Items)
}

func (s *Store) ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.DomainEvent, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		KeyConditionExpression: aws.String("PK = :pk"),
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":pk": &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("EVENT#%s#%s", aggregateType, aggregateID)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamostore: query by aggregate: %w", err)
	}
	return unmarshalEvents(out.Items)
}

func unmarshalEvents(items []map[string]ddbtypes.AttributeValue) ([]types.DomainEvent, error) {
	var events []types.DomainEvent
	for _, item := range items {
		var record struct {
			ID              string `dynamodbav:"ID"`
			AggregateType   string `dynamodbav:"AggregateType"`
			AggregateID     string `dynamodbav:"AggregateID"`
			WorkflowType    string `dynamodbav:"WorkflowType"`
			WorkflowVersion int    `dynamodbav:"WorkflowVersion"`
			EventType       string `dynamodbav:"EventType"`
			CorrelationID   string `dynamodbav:"CorrelationID"`
			ActorID         string `dynamodbav:"ActorID"`
			TransitionName  string `dynamodbav:"TransitionName"`
			StateBefore     string `dynamodbav:"StateBefore"`
			StateAfter      string `dynamodbav:"StateAfter"`
			Payload         string `dynamodbav:"Payload"`
			CreatedAt       string `dynamodbav:"CreatedAt"`
		}
		if err := attributevalue.UnmarshalMap(item, &record); err != nil {
			return nil, fmt.Errorf("dynamostore: unmarshal event: %w", err)
		}

		e := types.DomainEvent{
			ID: record.ID, AggregateType: record.AggregateType, AggregateID: record.AggregateID,
			WorkflowType: record.WorkflowType, WorkflowVersion: record.WorkflowVersion,
			EventType: record.EventType, CorrelationID: record.CorrelationID,
			ActorID: record.ActorID, TransitionName: record.TransitionName,
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, record.CreatedAt)
		_ = json.Unmarshal([]byte(record.StateBefore), &e.StateBefore)
		_ = json.Unmarshal([]byte(record.StateAfter), &e.StateAfter)
		_ = json.Unmarshal([]byte(record.Payload), &e.Payload)
		events = append(events, e)
	}
	return events, nil
}

// --- InstanceStore ---

func (s *Store) Get(ctx context.Context, aggregateType, aggregateID string) (*types.WorkflowInstance, error) {
	out, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]ddbtypes.AttributeValue{
			"PK": &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("INST#%s#%s", aggregateType, aggregateID)},
			"SK": &ddbtypes.AttributeValueMemberS{Value: "INSTANCE"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamostore: get instance: %w", err)
	}
	if out.Item == nil {
		return nil, s.errNotFound
	}

	var inst types.WorkflowInstance
	if err := attributevalue.UnmarshalMap(out.Item, &inst); err != nil {
		return nil, fmt.Errorf("dynamostore: unmarshal instance: %w", err)
	}
	return &inst, nil
}

func (s *Store) Create(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	item, err := attributevalue.MarshalMap(instance)
	if err != nil {
		return fmt.Errorf("dynamostore: marshal instance: %w", err)
	}
	item["PK"] = &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("INST#%s#%s", instance.AggregateType, instance.AggregateID)}
	item["SK"] = &ddbtypes.AttributeValueMemberS{Value: "INSTANCE"}
	item["Type"] = &ddbtypes.AttributeValueMemberS{Value: "Instance"}

	if tx != nil {
		t := tx.(*ddbTx)
		t.items = append(t.items, ddbtypes.TransactWriteItem{
			Put: &ddbtypes.Put{
				TableName: aws.String(s.tableName),
				Item:      item,
			},
		})
		return nil
	}

	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	return err
}

func (s *Store) Update(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	item, err := attributevalue.MarshalMap(instance)
	if err != nil {
		return fmt.Errorf("dynamostore: marshal instance: %w", err)
	}
	item["PK"] = &ddbtypes.AttributeValueMemberS{Value: fmt.Sprintf("INST#%s#%s", instance.AggregateType, instance.AggregateID)}
	item["SK"] = &ddbtypes.AttributeValueMemberS{Value: "INSTANCE"}
	item["Type"] = &ddbtypes.AttributeValueMemberS{Value: "Instance"}

	oldUpdatedAt := instance.LastReadUpdatedAt.Format(time.RFC3339Nano)
	condExpr := aws.String("UpdatedAt = :oldUpdatedAt")
	exprValues := map[string]ddbtypes.AttributeValue{
		":oldUpdatedAt": &ddbtypes.AttributeValueMemberS{Value: oldUpdatedAt},
	}

	if tx != nil {
		t := tx.(*ddbTx)
		t.items = append(t.items, ddbtypes.TransactWriteItem{
			Put: &ddbtypes.Put{
				TableName:                 aws.String(s.tableName),
				Item:                      item,
				ConditionExpression:       condExpr,
				ExpressionAttributeValues: exprValues,
			},
		})
		return nil
	}

	_, putErr := s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 aws.String(s.tableName),
		Item:                      item,
		ConditionExpression:       condExpr,
		ExpressionAttributeValues: exprValues,
	})
	if putErr != nil {
		var condFailed *ddbtypes.ConditionalCheckFailedException
		if errors.As(putErr, &condFailed) {
			return fmt.Errorf("dynamostore: update instance: %w", flowstate.ErrConcurrentModification)
		}
		return fmt.Errorf("dynamostore: update instance: %w", putErr)
	}
	return nil
}

func (s *Store) ListStuck(ctx context.Context) ([]types.WorkflowInstance, error) {
	// Requires a GSI on IsStuck — simplified scan for now
	out, err := s.client.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(s.tableName),
		FilterExpression: aws.String("#type = :type AND IsStuck = :stuck"),
		ExpressionAttributeNames: map[string]string{
			"#type": "Type",
		},
		ExpressionAttributeValues: map[string]ddbtypes.AttributeValue{
			":type":  &ddbtypes.AttributeValueMemberS{Value: "Instance"},
			":stuck": &ddbtypes.AttributeValueMemberBOOL{Value: true},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamostore: scan stuck: %w", err)
	}

	var instances []types.WorkflowInstance
	for _, item := range out.Items {
		var inst types.WorkflowInstance
		if err := attributevalue.UnmarshalMap(item, &inst); err != nil {
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}
