package core

import (
	"context"

	"github.com/shopspring/decimal"
)

type (
	SnapshotStore interface {
		UpsertSnapshot(ctx context.Context, snapshot *Snapshot) error
		GetSnapshotCount(ctx context.Context) (int64, error)
		InsertSnapshot(ctx context.Context, snapshot *Snapshot) error
		GetSnapshotById(ctx context.Context, snapshotId string) (*Snapshot, error)
		GetLastestSnapshot(ctx context.Context) (*Snapshot, error)
	}

	Snapshot struct {
		SnapshotId string          `json:"snapshotId"`
		RequestId  string          `json:"requestId"`
		UserId     string          `json:"userId"`
		AssetId    string          `json:"assetId"`
		Amount     decimal.Decimal `json:"amount"`
		Memo       string          `json:"memo"`
		CreatedAt  int64           `json:"createdAt"`
	}
)
