package pgxstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxProvider implements flowstep.TxProvider using pgx transactions.
type TxProvider struct {
	pool *pgxpool.Pool
}

// NewTxProvider creates a new TxProvider backed by the given connection pool.
func NewTxProvider(pool *pgxpool.Pool) *TxProvider {
	return &TxProvider{pool: pool}
}

func (p *TxProvider) Begin(ctx context.Context) (any, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: begin tx: %w", err)
	}
	return tx, nil
}

func (p *TxProvider) Commit(ctx context.Context, tx any) error {
	pgxTx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgxstore: invalid tx type")
	}
	return pgxTx.Commit(ctx)
}

func (p *TxProvider) Rollback(ctx context.Context, tx any) error {
	pgxTx, ok := tx.(pgx.Tx)
	if !ok {
		return fmt.Errorf("pgxstore: invalid tx type")
	}
	return pgxTx.Rollback(ctx)
}

func getTx(tx any) (pgx.Tx, error) {
	if tx == nil {
		return nil, fmt.Errorf("pgxstore: nil tx")
	}
	pgxTx, ok := tx.(pgx.Tx)
	if !ok {
		return nil, fmt.Errorf("pgxstore: invalid tx type")
	}
	return pgxTx, nil
}
