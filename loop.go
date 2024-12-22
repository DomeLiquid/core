package core

import (
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type LoopPaymentOptions struct {
	Type           LoopPaymentType `json:"type,omitempty"`
	TargetLeverage decimal.Decimal `json:"targetLeverage,omitempty"`
	DepositBankId  uuid.UUID       `json:"depositBankId,omitempty"`
	BorrowBankId   uuid.UUID       `json:"borrowBankId,omitempty"`
	DepositAmount  decimal.Decimal `json:"depositAmount,omitempty"`

	LoopStep1 *LoopPaymentStep  `json:"loopStep1,omitempty"`
	LoopStep2 *LoopPaymentStep  `json:"loopStep2,omitempty"`
	LoopStep3 *LoopPaymentStep3 `json:"loopStep3,omitempty"`
	LoopStep4 *LoopPaymentStep  `json:"loopStep4,omitempty"`
}

type LoopPaymentType string

const (
	LoopPaymentTypeLong  LoopPaymentType = "long"
	LoopPaymentTypeShort LoopPaymentType = "short"
)

type LoopPaymentStep struct {
	Action  MemoActionType  `json:"action,omitempty"`
	BankId  uuid.UUID       `json:"bankId,omitempty"`
	Amount  decimal.Decimal `json:"amount,omitempty"`
	State   PaymentStatus   `json:"state,omitempty"`
	Message string          `json:"message,omitempty"`
}

type LoopPaymentStep3 struct {
	InputBankId      uuid.UUID        `json:"inputBankId,omitempty"`
	OutputBankId     uuid.UUID        `json:"outputBankId,omitempty"`
	OrderId          string           `json:"orderId,omitempty"`
	State            PaymentStatus    `json:"state,omitempty"`
	SwapResponseView SwapResponseView `json:"swapResponseView,omitempty"`
}

// PmtOptFunc is a function that can be used to modify a payment
type PmtOptFunc func(payment *Payment)

func WithMetaMap(metaMap *MetaMap) PmtOptFunc {
	return func(payment *Payment) {
		payment.Extra.MetaMap = metaMap
	}
}

func WithLoopOptions(loopOptions *LoopPaymentOptions) PmtOptFunc {
	return func(payment *Payment) {
		payment.Extra.LoopOptions = loopOptions
	}
}

func WithClosePositionResult(closePositionResult *ClosePositionResult) PmtOptFunc {
	return func(payment *Payment) {
		payment.Extra.ClosePositionResult = closePositionResult
	}
}

func NewLoopPaymentStep(action MemoActionType, bankId uuid.UUID, amount decimal.Decimal) *LoopPaymentStep {
	step := &LoopPaymentStep{
		Action: action,
		BankId: bankId,
		Amount: amount,
		State:  PaymentStatusPending,
	}
	return step
}

func NewLoopPaymentStep3(inputBankId, outputBankId uuid.UUID, orderId string, swapResponseView SwapResponseView) *LoopPaymentStep3 {
	step := &LoopPaymentStep3{
		InputBankId:      inputBankId,
		OutputBankId:     outputBankId,
		OrderId:          orderId,
		SwapResponseView: swapResponseView,
		State:            PaymentStatusPending,
	}
	return step
}
