package repository

import (
	"context"
	"github.com/TicketsBot/export/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func Connect(ctx context.Context, conf config.DatabaseConfig) (*Repository, error) {
	pool, err := pgxpool.New(ctx, conf.Uri)
	if err != nil {
		return nil, err
	}

	return NewRepository(pool), nil
}

func (r *Repository) Tx(ctx context.Context, f func(ctx context.Context, tx TransactionContext) error) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}

	transactionContext := &PostgresTransactionContext{tx: tx}

	defer tx.Rollback(ctx)

	if err := f(ctx, transactionContext); err != nil {
		transactionContext.aborted = true
		return err
	}

	if transactionContext.aborted {
		return tx.Rollback(ctx)
	} else {
		return tx.Commit(ctx)
	}
}
