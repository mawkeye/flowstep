package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

func newRelation(id, parentType, parentID, childType, childID string) types.ChildRelation {
	return types.ChildRelation{
		ID:                  id,
		ParentWorkflowType:  "wf",
		ParentAggregateType: parentType,
		ParentAggregateID:   parentID,
		ChildWorkflowType:   "child-wf",
		ChildAggregateType:  childType,
		ChildAggregateID:    childID,
		Status:              types.ChildStatusActive,
		CreatedAt:           time.Now(),
	}
}

func TestChildStoreCreateAndGetByChild(t *testing.T) {
	s := NewChildStore()
	ctx := context.Background()
	rel := newRelation("r-1", "order", "o-1", "shipment", "s-1")

	if err := s.Create(ctx, nil, rel); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetByChild(ctx, "shipment", "s-1")
	if err != nil {
		t.Fatalf("get by child: %v", err)
	}
	if got.ID != "r-1" {
		t.Errorf("expected r-1, got %s", got.ID)
	}
}

func TestChildStoreGetByChildNotFound(t *testing.T) {
	s := NewChildStore()
	_, err := s.GetByChild(context.Background(), "shipment", "nope")
	if err == nil {
		t.Error("expected error for missing child, got nil")
	}
}

func TestChildStoreGetByParent(t *testing.T) {
	s := NewChildStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newRelation("r-1", "order", "o-1", "shipment", "s-1"))
	_ = s.Create(ctx, nil, newRelation("r-2", "order", "o-1", "shipment", "s-2"))
	_ = s.Create(ctx, nil, newRelation("r-3", "order", "o-2", "shipment", "s-3"))

	relations, err := s.GetByParent(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("get by parent: %v", err)
	}
	if len(relations) != 2 {
		t.Errorf("expected 2 relations for o-1, got %d", len(relations))
	}
}

func TestChildStoreGetByGroup(t *testing.T) {
	s := NewChildStore()
	ctx := context.Background()

	r1 := newRelation("r-1", "order", "o-1", "shipment", "s-1")
	r1.GroupID = "grp-1"
	r2 := newRelation("r-2", "order", "o-1", "shipment", "s-2")
	r2.GroupID = "grp-1"
	r3 := newRelation("r-3", "order", "o-1", "shipment", "s-3")
	r3.GroupID = "grp-2"

	_ = s.Create(ctx, nil, r1)
	_ = s.Create(ctx, nil, r2)
	_ = s.Create(ctx, nil, r3)

	group, err := s.GetByGroup(ctx, "grp-1")
	if err != nil {
		t.Fatalf("get by group: %v", err)
	}
	if len(group) != 2 {
		t.Errorf("expected 2 in grp-1, got %d", len(group))
	}
}

func TestChildStoreComplete(t *testing.T) {
	s := NewChildStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newRelation("r-1", "order", "o-1", "shipment", "s-1"))

	if err := s.Complete(ctx, nil, "shipment", "s-1", "shipped"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, _ := s.GetByChild(ctx, "shipment", "s-1")
	if got.Status != types.ChildStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.ChildTerminalState != "shipped" {
		t.Errorf("expected ChildTerminalState=shipped, got %s", got.ChildTerminalState)
	}
	if got.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
}

func TestChildStoreCompleteNotFound(t *testing.T) {
	s := NewChildStore()
	err := s.Complete(context.Background(), nil, "shipment", "nope", "done")
	if err == nil {
		t.Error("expected error for missing child, got nil")
	}
}
