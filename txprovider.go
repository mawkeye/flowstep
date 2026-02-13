package flowstate

import "context"

// TxProvider manages database transactions.
type TxProvider interface {
	Begin(ctx context.Context) (tx any, err error)
	Commit(ctx context.Context, tx any) error
	Rollback(ctx context.Context, tx any) error
}
