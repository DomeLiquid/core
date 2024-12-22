package core

import (
	"context"

	"github.com/facebookgo/clock"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type (
	BalanceStore interface {
		FindBalance(ctx context.Context, bankId, accountId uuid.UUID) (*Balance, error)
		UpsertBalance(ctx context.Context, balance *Balance) error
		ListBalances(ctx context.Context, accountId, bankId uuid.UUID) ([]*Balance, error)
	}

	Balance struct {
		AccountId uuid.UUID `json:"accountId"`
		BankId    uuid.UUID `json:"bankId"`

		Active               bool            `json:"active"`
		AssetShares          decimal.Decimal `json:"assetShares"`
		LiabilityShares      decimal.Decimal `json:"liabilityShares"`
		EmissionsOutstanding decimal.Decimal `json:"emissionsOutstanding"`
		LastUpdate           int64           `json:"lastUpdate"`
	}
)

func NewBalance(clk clock.Clock, accountId, bankId uuid.UUID) *Balance {
	return &Balance{
		AccountId: accountId,
		BankId:    bankId,

		Active:               true,
		AssetShares:          decimal.Zero,
		LiabilityShares:      decimal.Zero,
		EmissionsOutstanding: decimal.Zero,
		LastUpdate:           clk.Now().Unix(),
	}
}

func FindOrCreateBalance(ctx context.Context, clk clock.Clock, bankAccountService BankAccountService, bank *Bank, account *Account) (*Balance, error) {
	_, err := bankAccountService.GetBankById(ctx, bank.Id)
	if err != nil {
		return nil, BankAccountNotFound
	}

	balance, err := bankAccountService.FindBalance(ctx, bank.Id, account.Id)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			balance = NewBalance(clk, account.Id, bank.Id)
			err = bankAccountService.UpsertBalance(ctx, balance)
			if err != nil {
				return nil, err
			}
			return balance, nil
		}
		return nil, err
	}
	return balance, nil
}

func (b *Balance) Clone() *Balance {
	return &Balance{
		AccountId:            b.AccountId,
		BankId:               b.BankId,
		Active:               b.Active,
		AssetShares:          b.AssetShares,
		LiabilityShares:      b.LiabilityShares,
		EmissionsOutstanding: b.EmissionsOutstanding,
		LastUpdate:           b.LastUpdate,
	}
}

// 判断账户是否为空
func (b *Balance) IsEmpty(side BalanceSide) bool {
	switch side {
	case BalanceSideAssets:
		return b.AssetShares.LessThan(EMPTY_BALANCE_THRESHOLD)
	case BalanceSideLiabilities:
		return b.LiabilityShares.LessThan(EMPTY_BALANCE_THRESHOLD)
	default:
		return true
	}
}

// 改变资产份额
func (b *Balance) ChangeAssetShares(delta decimal.Decimal) error {
	assetShares := b.AssetShares
	assetShares = assetShares.Add(delta)
	if assetShares.LessThan(decimal.Zero) {
		return BankLiabilityCapacityExceeded
	}
	b.AssetShares = assetShares
	return nil
}

// 改变负债份额
func (b *Balance) ChangeLiabilityShares(delta decimal.Decimal) error {
	liabilityShares := b.LiabilityShares
	liabilityShares = liabilityShares.Add(delta)

	if liabilityShares.LessThan(decimal.Zero) {
		return BankLiabilityCapacityExceeded
	}
	b.LiabilityShares = liabilityShares
	return nil
}

// 关闭账户
func (b *Balance) Close(clk clock.Clock) error {
	if b.EmissionsOutstanding.GreaterThanOrEqual(EMPTY_BALANCE_THRESHOLD) {
		return CannotCloseOutstandingEmissions
	}
	b.EmptyDeactivated(clk)
	return nil
}

func (b *Balance) GetSide() (BalanceSide, error) {
	assetShares := b.AssetShares
	liabilityShares := b.LiabilityShares

	// 确保资产份额和负债份额有一个为zero
	if assetShares.GreaterThan(ZERO_AMOUNT_THRESHOLD) && liabilityShares.GreaterThan(ZERO_AMOUNT_THRESHOLD) {
		return BalanceSideEmpty, IllegalBalanceState
	}

	if assetShares.GreaterThanOrEqual(EMPTY_BALANCE_THRESHOLD) {
		return BalanceSideAssets, nil
	}

	if liabilityShares.GreaterThanOrEqual(EMPTY_BALANCE_THRESHOLD) {
		return BalanceSideLiabilities, nil
	}

	return BalanceSideEmpty, nil
}

func (b *Balance) EmptyDeactivated(clk clock.Clock) {
	b.Active = false
	b.AssetShares = decimal.Zero
	b.LiabilityShares = decimal.Zero
	b.EmissionsOutstanding = decimal.Zero
	b.LastUpdate = clk.Now().Unix()
}

func (b *Balance) ComputeUsdValue(bank *Bank, oraclePrice decimal.Decimal, requirementType RequirementType) (decimal.Decimal, decimal.Decimal) {
	assetsValue := bank.ComputeAssetUsdValue(oraclePrice, b.AssetShares, requirementType, Original)
	liabilitiesValue := bank.ComputeLiabilityUsdValue(oraclePrice, b.LiabilityShares, requirementType, Original)
	return assetsValue, liabilitiesValue
}

func (b *Balance) GetUsdValueWithPriceBias(bank *Bank, oraclePrice decimal.Decimal, requirementType RequirementType) (decimal.Decimal, decimal.Decimal) {
	assetsValue := bank.ComputeAssetUsdValue(oraclePrice, b.AssetShares, requirementType, Low)
	liabilitiesValue := bank.ComputeLiabilityUsdValue(oraclePrice, b.LiabilityShares, requirementType, High)
	return assetsValue, liabilitiesValue
}

func (b *Balance) ComputeQuantity(bank *Bank) (decimal.Decimal, decimal.Decimal) {
	assetsQuantity := bank.GetAssetQuantity(b.AssetShares)
	liabilitiesQuantity := bank.GetLiabilityQuantity(b.LiabilityShares)
	return assetsQuantity, liabilitiesQuantity
}

type BalanceIncreaseType uint8

const (
	BalanceIncreaseTypeAny                BalanceIncreaseType = 1 << 0
	BalanceIncreaseTypeRepayOnly          BalanceIncreaseType = 1 << 1
	BalanceIncreaseTypeDepositOnly        BalanceIncreaseType = 1 << 2
	BalanceIncreaseTypeBypassDepositLimit BalanceIncreaseType = 1 << 3
)

func (b BalanceIncreaseType) String() string {
	switch b {
	case BalanceIncreaseTypeAny:
		return "Any"
	case BalanceIncreaseTypeRepayOnly:
		return "RepayOnly"
	case BalanceIncreaseTypeDepositOnly:
		return "DepositOnly"
	case BalanceIncreaseTypeBypassDepositLimit:
		return "BypassDepositLimit"
	default:
		return "Unknown"
	}
}

type BalanceDecreaseType uint8

// 定义 BalanceDecreaseType 类型的常量，用于表示不同的余额减少操作类型
const (
	// BalanceDecreaseTypeAny 表示任意类型的余额减少操作
	BalanceDecreaseTypeAny BalanceDecreaseType = 1 << 0

	// BalanceDecreaseTypeWithdrawOnly 表示仅提款操作
	BalanceDecreaseTypeWithdrawOnly BalanceDecreaseType = 1 << 1

	// BalanceDecreaseTypeBorrowOnly 表示仅借款操作
	BalanceDecreaseTypeBorrowOnly BalanceDecreaseType = 1 << 2

	// BalanceDecreaseTypeBypassBorrowLimit 表示绕过借款限制的操作
	BalanceDecreaseTypeBypassBorrowLimit BalanceDecreaseType = 1 << 3
)

func (b BalanceDecreaseType) String() string {
	switch b {
	case BalanceDecreaseTypeAny:
		return "Any"
	case BalanceDecreaseTypeWithdrawOnly:
		return "WithdrawOnly"
	case BalanceDecreaseTypeBorrowOnly:
		return "BorrowOnly"
	case BalanceDecreaseTypeBypassBorrowLimit:
		return "BypassBorrowLimit"
	default:
		return "Unknown"
	}
}
