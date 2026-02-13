package memstore

import "context"

// TxProvider is a no-op transaction provider for in-memory stores.
type TxProvider struct{}

// NewTxProvider creates a new no-op TxProvider.
func NewTxProvider() *TxProvider { return &TxProvider{} }

func (*TxProvider) Begin(_ context.Context) (any, error) { return struct{}{}, nil }
func (*TxProvider) Commit(_ context.Context, _ any) error { return nil }
func (*TxProvider) Rollback(_ context.Context, _ any) error { return nil }
