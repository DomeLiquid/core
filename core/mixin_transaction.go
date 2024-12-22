package core

import "context"

type (
	MixinTransactionStore interface {
		CreateMixinTransaction(ctx context.Context, transaction *MixinTransaction) error
		UpdateMixinTransactionStatus(ctx context.Context, requestId string, status MixinTransactionStatus) error
		GetMixinTransaction(ctx context.Context, requestId string) (*MixinTransaction, error)
	}

	MixinTransaction struct {
		RequestId string                 `json:"requestId"`
		PaymentId string                 `json:"paymentId"`
		Uid       string                 `json:"uid"`
		Status    MixinTransactionStatus `json:"status"`
		Memo      string                 `json:"memo"`

		CreatedAt int64 `json:"createdAt"`
		UpdatedAt int64 `json:"updatedAt"`
	}

	MixinTransactionStatus string
)

const (
	MixinTransactionStatusPending   MixinTransactionStatus = "pending"
	MixinTransactionStatusConfirmed MixinTransactionStatus = "confirmed"
	MixinTransactionStatusFailed    MixinTransactionStatus = "failed"
)
