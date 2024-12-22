package core

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/facebookgo/clock"
	"github.com/fox-one/mixin-sdk-go/v2"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type (
	MixinStore interface {
		ChainStore
		MixinAccountStore
		MixinOracleStore
	}

	ChainStore interface {
		GetChain(ctx context.Context, chainId string) (*Chain, error)
		UpsertChain(ctx context.Context, chain *Chain) error
		ListChains(ctx context.Context) ([]*Chain, error)
	}

	MixinChain struct {
		ChainId string `json:"chain_id"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
		IconURL string `json:"icon_url"`
	}

	Chain struct {
		ChainId string `json:"chainId"`
		Name    string `json:"name"`
		Symbol  string `json:"symbol"`
		IconURL string `json:"iconUrl"`
	}
)

func NewChainFromMixin(chain MixinChain) *Chain {
	return &Chain{
		ChainId: chain.ChainId,
		Name:    chain.Name,
		Symbol:  chain.Symbol,
		IconURL: chain.IconURL,
	}
}

type (
	MixinAccount struct {
		Uid            string
		IdentityNumber string
		FullName       string
		AvatarURL      string
		SessionID      string
		Biography      string
		MixinCreatedAt int64
		CreatedAt      int64
		UpdatedAt      int64
		AccessToken    string
	}

	MixinAccountStore interface {
		ListAllMixinAccount(ctx context.Context) ([]*MixinAccount, error)
		GetMixinAccount(ctx context.Context, uid string) (*MixinAccount, error)
		UpsertMixinAccount(ctx context.Context, uid string, account *MixinAccount) error
	}
)

func NewMixinAccountFromMixin(clk clock.Clock, user *mixin.User, accessToken string) *MixinAccount {
	return &MixinAccount{
		IdentityNumber: user.IdentityNumber,
		Uid:            user.UserID,
		FullName:       user.FullName,
		AvatarURL:      user.AvatarURL,
		SessionID:      user.SessionID,
		Biography:      user.Biography,
		MixinCreatedAt: user.CreatedAt.Unix(),
		CreatedAt:      user.CreatedAt.Unix(),
		UpdatedAt:      clk.Now().Unix(),
		AccessToken:    accessToken,
	}
}

type MemoActionType uint8

const (
	MATSupply MemoActionType = iota + 1
	MATBorrow
	MATRepay
	MATWithdraw
	MATLoop
	MATDomeLoopClosePosition // for dome loop
	MATLiquidate             // TODO
	// MATWithdrawEmissions // SettleEmissions + Withdraw
	// MATAccrueBankInterest
	// MATWithdrawFees
	// MATWithdrawInsurance
	// MATCollectBankFees
	// MATCloseBalance
	// MATSettleEmissions
	// MATBankruptcy
	// MATSetAccountFlag
	// MATUnsetAccountFlag
	// MATAccountClose // TODO
)

func (m MemoActionType) String() string {
	switch m {
	case MATSupply:
		return "Supply"
	case MATRepay:
		return "Repay"
	case MATWithdraw:
		return "Withdraw"
	case MATBorrow:
		return "Borrow"
	case MATLiquidate:
		return "Liquidate"
	case MATLoop:
		return "Loop"
	case MATDomeLoopClosePosition:
		return "Dome Loop Close Position"
	// case MATWithdrawEmissions:
	// 	return "Withdraw Emissions"
	// case MATAccrueBankInterest:
	// 	return "Accrue Bank Interest"
	// case MATWithdrawFees:
	// 	return "Withdraw Fees"
	// case MATWithdrawInsurance:
	// 	return "Withdraw Insurance"
	default:
		return "Unknown"
	}
}

func ValidActionTypeString(action string) (MemoActionType, bool) {
	switch action {
	case MATSupply.String():
		return MATSupply, true
	case MATBorrow.String():
		return MATBorrow, true
	case MATRepay.String():
		return MATRepay, true
	case MATWithdraw.String():
		return MATWithdraw, true
	case MATLiquidate.String():
		return MATLiquidate, true
	case MATLoop.String():
		return MATLoop, true
	case MATDomeLoopClosePosition.String():
		return MATDomeLoopClosePosition, true
	// case MATWithdrawEmissions.String():
	// 	return MATWithdrawEmissions, true
	// case MATAccrueBankInterest.String():
	// 	return MATAccrueBankInterest, true
	// case MATWithdrawFees.String():
	// 	return MATWithdrawFees, true
	// case MATWithdrawInsurance.String():
	// 	return MATWithdrawInsurance, true
	default:
		return 0, false
	}
}

func (m MemoActionType) Valid() bool {
	switch m {
	case MATSupply,
		MATRepay,
		MATWithdraw,
		MATBorrow,
		MATLiquidate,
		MATLoop,
		MATDomeLoopClosePosition:
		// MATWithdrawEmissions,
		// MATAccrueBankInterest,
		// MATWithdrawFees,
		// MATWithdrawInsurance,
		return true
	default:
		return false
	}
}

type MemoAction struct {
	AccountIndex uint8          `json:"i"`
	ActionType   MemoActionType `json:"t"`
}

type MemoActionSupply struct {
	MemoAction `json:",inline"`
	BankId     uuid.UUID       `json:"b"`
	Amount     decimal.Decimal `json:"a"`
}

func (m MemoAction) Valid() bool {
	if !m.ActionType.Valid() {
		return false
	}
	if m.AccountIndex > 255 {
		return false
	}
	return true
}

type MemoActionWithdraw struct {
	MemoAction
	BankId      uuid.UUID       `json:"b"`
	Amount      decimal.Decimal `json:"a"`
	WithdrawAll bool            `json:"wa"`
}

type MemoActionWithdrawEmissions struct {
	MemoAction
}

type MemoActionRepay struct {
	MemoAction
	BankId   uuid.UUID       `json:"b"`
	Amount   decimal.Decimal `json:"a"`
	RepayAll bool            `json:"ra"`
}

type MemoActionBorrow struct {
	MemoAction
	BankId uuid.UUID       `json:"b"`
	Amount decimal.Decimal `json:"a"`
}

type MemoActionClosePosition struct {
	MemoAction
	GroupId uuid.UUID `json:"g"`
}

type MemoActionLiquidate struct {
	MemoAction
	BankId              uuid.UUID `json:"b"`
	LiquidateeAccountId uuid.UUID `json:"la"`
	LiabilityBankId     uuid.UUID `json:"lb"`
}

func (m MemoActionLiquidate) Valid() bool {
	if !m.MemoAction.Valid() {
		return false
	}
	if m.ActionType != MATLiquidate {
		return false
	}
	if m.LiabilityBankId == uuid.Nil || m.LiquidateeAccountId == uuid.Nil {
		return false
	}
	return true
}

type MemoActionWithdrawFees struct {
	MemoAction
	Amount decimal.Decimal `json:"a"`
}

type MemoActionWithdrawInsurance struct {
	Amount decimal.Decimal `json:"a"`
}

type MemoActionLoop struct {
	MemoAction
	BankId         uuid.UUID       `json:"b"`
	BorrowBankId   uuid.UUID       `json:"bb"`
	TargetLeverage decimal.Decimal `json:"tl"`
}

func (m MemoActionLoop) Valid() bool {
	if !m.MemoAction.Valid() {
		return false
	}

	if m.ActionType != MATLoop {
		return false
	}
	if m.BankId == uuid.Nil || m.BorrowBankId == uuid.Nil {
		return false
	}

	if m.BankId.String() == m.BorrowBankId.String() {
		return false
	}

	if m.ActionType != MATLoop {
		return false
	}

	if m.AccountIndex != 0 {
		return false
	}

	if !m.TargetLeverage.IsPositive() || m.TargetLeverage.LessThanOrEqual(ONE) {
		return false
	}

	return true
}

func EncodeAnyMemo(a any) (string, error) {
	bytes, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(bytes), nil
}

func DecodeSnapshotMemo(memo string) (*MemoAction, error) {
	snapshotMemoHex, err := hex.DecodeString(memo)
	if err != nil {
		return nil, err
	}

	snapshotMemo, err := base64.StdEncoding.DecodeString(string(snapshotMemoHex))
	if err != nil {
		return nil, err
	}

	var memoAction MemoAction
	err = json.Unmarshal([]byte(snapshotMemo), &memoAction)
	if err != nil {
		return nil, err
	}

	return &memoAction, nil
}

func DecodeSnapshotMemoAny(memo string, res any) error {
	snapshotMemoHex, err := hex.DecodeString(memo)
	if err != nil {
		return err
	}

	snapshotMemo, err := base64.StdEncoding.DecodeString(string(snapshotMemoHex))
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(snapshotMemo), res)
}

func IsMixinOrderTransferMemo(memo string) (string, bool) {
	snapshotMemo, err := hex.DecodeString(memo)
	if err != nil {
		return "", false
	}

	// {order_id}#transfer
	if strings.HasSuffix(string(snapshotMemo), "#transfer") {
		return strings.TrimSuffix(string(snapshotMemo), "#transfer"), true
	}

	return "", false
}

func IsMixinOrderRefundMemo(memo string) (string, bool) {
	snapshotMemo, err := hex.DecodeString(memo)
	if err != nil {
		return "", false
	}

	// {order_id}#{uuid}#{refund}
	if strings.HasSuffix(string(snapshotMemo), "#refund") {
		parts := strings.Split(string(snapshotMemo), "#")
		if len(parts) == 3 {
			return parts[0], true
		}
	}
	return "", false
}
