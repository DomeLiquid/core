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
	OperateStore interface {
		CreateOperate(ctx context.Context, operate *Operate) error
		ListOperates(ctx context.Context, pubKey string, op MemoActionType, createdBeforeAt, limit int64) ([]Operate, error)
	}

	Operate struct {
		PubKey    string         `json:"pubKey"`
		AccountId uuid.UUID      `json:"accountId"`
		Op        MemoActionType `json:"op"`
		Extra     OperateDetail  `json:"extra"`
		CreatedAt int64          `json:"createdAt"`
	}

	OperateDetail struct {
		Type      MemoActionType `json:"type"`
		AccountId uuid.UUID      `json:"actor"`
		Actions   []ActionDetail `json:"actions"`
	}

	ActionDetail struct {
		AccountId  uuid.UUID       `json:"actor"`
		ActionType MemoActionType  `json:"actionType"`
		BankId     uuid.UUID       `json:"bankId"`
		Amount     decimal.Decimal `json:"amount"`
	}
)

func NewOperate(clk clock.Clock, pubKey string, accountId uuid.UUID, typ MemoActionType, extra OperateDetail) Operate {
	return Operate{
		PubKey:    pubKey,
		AccountId: accountId,
		Op:        typ,
		Extra:     extra,
		CreatedAt: clk.Now().Unix(),
	}
}

func (j OperateDetail) Value() (driver.Value, error) {
	valueString, err := json.Marshal(j)
	return string(valueString), err
}

func (j *OperateDetail) Scan(value any) error {
	if err := json.Unmarshal(value.([]byte), &j); err != nil {
		return err
	}
	return nil
}

func (p Payment) OperationDetail() OperateDetail {
	actions := []ActionDetail{}
	switch p.Action {
	case MATLoop:
		if p.Extra.LoopOptions == nil || p.Extra.LoopOptions.LoopStep1 == nil || p.Extra.LoopOptions.LoopStep2 == nil {
			return OperateDetail{}
		}

		actions = append(actions, ActionDetail{
			AccountId:  p.AccountId,
			ActionType: p.Extra.LoopOptions.LoopStep1.Action,
			BankId:     p.Extra.LoopOptions.LoopStep1.BankId,
			Amount:     p.Extra.LoopOptions.LoopStep1.Amount,
		}, ActionDetail{
			AccountId:  p.AccountId,
			ActionType: p.Extra.LoopOptions.LoopStep2.Action,
			BankId:     p.Extra.LoopOptions.LoopStep2.BankId,
			Amount:     p.Extra.LoopOptions.LoopStep2.Amount,
		})
	case MATLiquidate:
	case MATDomeLoopClosePosition:
	default:
		actions = append(actions, ActionDetail{
			AccountId:  p.AccountId,
			ActionType: p.Action,
			BankId:     p.BankId,
			Amount:     p.Amount,
		})
	}

	return OperateDetail{
		Type:      p.Action,
		AccountId: p.AccountId,
		Actions:   actions,
	}
}
