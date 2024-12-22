package core

import (
	"context"
	"database/sql/driver"
	"encoding/json"

	"github.com/facebookgo/clock"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type (
	PaymentStore interface {
		CreatePayment(ctx context.Context, payment *Payment) error
		UpsertPayment(ctx context.Context, payment *Payment) error
		UpdatePaymentStatus(ctx context.Context, requestId string, status PaymentStatus, message string, updatedAt int64) error
		GetPaymentByRequestId(ctx context.Context, requestId string) (*Payment, error)
		GetPaymentByMixinOrderId(ctx context.Context, orderId string) (*Payment, error)
	}

	Payment struct {
		RequestId    string        `json:"requestId"`
		MixinOrderId string        `json:"mixinOrderId,omitempty"`
		Uid          string        `json:"uid"`
		Status       PaymentStatus `json:"status"`
		Message      string        `json:"message"`

		BankId    uuid.UUID       `json:"bankId"`
		AccountId uuid.UUID       `json:"accountId"`
		Action    MemoActionType  `json:"action"`
		Amount    decimal.Decimal `json:"amount"`

		Extra     PaymentExtra `json:"extra,omitempty"`
		CreatedAt int64        `json:"createdAt"`
		UpdatedAt int64        `json:"updatedAt"`
	}

	PaymentExtra struct {
		MetaMap             *MetaMap             `json:"metaMap,omitempty"`
		LoopOptions         *LoopPaymentOptions  `json:"loopOptions,omitempty"`
		LiquidateResult     *LiquidateResult     `json:"liquidateResult,omitempty"`
		ClosePositionResult *ClosePositionResult `json:"closePosition,omitempty"`
	}
)

func NewPayment(clk clock.Clock,
	requestId string,
	uid string,
	bankId,
	accountId uuid.UUID,
	action MemoActionType,
	amount decimal.Decimal,
	assetId string,
	// meta string,
	opts ...PmtOptFunc,
) *Payment {
	payment := &Payment{
		RequestId: requestId,
		Uid:       uid,
		Status:    PaymentStatusPending,
		BankId:    bankId,
		AccountId: accountId,
		Action:    action,
		Amount:    amount,
		// Meta:      meta,

		CreatedAt: clk.Now().Unix(),
		UpdatedAt: clk.Now().Unix(),
	}
	for _, opt := range opts {
		opt(payment)
	}
	return payment
}

func (j PaymentExtra) Value() (driver.Value, error) {
	valueString, err := json.Marshal(j)
	return string(valueString), err
}

func (j *PaymentExtra) Scan(value any) error {
	if err := json.Unmarshal(value.([]byte), &j); err != nil {
		return err
	}
	return nil
}

type PaymentStatus string

const (
	PaymentStatusPending   PaymentStatus = "pending"
	PaymentStatusConfirmed PaymentStatus = "confirmed"
	PaymentStatusFailed    PaymentStatus = "failed"
)

func (p PaymentStatus) String() string {
	switch p {
	case PaymentStatusPending:
		return "pending"
	case PaymentStatusConfirmed:
		return "confirmed"
	case PaymentStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func (p *Payment) UpdateStatus(clk clock.Clock, status PaymentStatus, message string) {
	p.Status = status
	p.Message = message
	p.UpdatedAt = clk.Now().Unix()
}

type MetaMap struct {
	RepayAll    bool `json:"repay_all,omitempty"`
	WithdrawAll bool `json:"withdraw_all,omitempty"`
}

func (p *Payment) FillAction(uid string, action MemoActionType, amount decimal.Decimal, bankId uuid.UUID, accountId uuid.UUID) {
	if len(p.Uid) == 0 {
		p.Uid = uid
	}
	if p.Action == 0 {
		p.Action = action
	}
	if p.Amount.IsZero() {
		p.Amount = amount
	}
	if p.BankId == uuid.Nil {
		p.BankId = bankId
	}
	if p.AccountId == uuid.Nil {
		p.AccountId = accountId
	}
}

func (p Payment) IsVaild(uid string, bankId, accountId uuid.UUID, action MemoActionType, amount decimal.Decimal) bool {
	if p.Uid != uid {
		return false
	}
	if action == MATSupply && p.BankId.String() != bankId.String() {
		return false
	}
	if p.Action != action {
		return false
	}
	if action == MATSupply && !p.Amount.Equal(amount) {
		return false
	}
	return true
}

type ClosePositionResult struct {
	GroupId                  uuid.UUID       `json:"groupId"`
	DepositBankId            uuid.UUID       `json:"depositBankId"`
	BorrowBankId             uuid.UUID       `json:"borrowBankId"`
	RefundBorrowAssetAmount  decimal.Decimal `json:"refundBorrowAssetAmount"`
	RefundDepositAssetAmount decimal.Decimal `json:"refundDepositAssetAmount"`
}
