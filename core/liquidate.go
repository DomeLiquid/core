package core

import (
	"github.com/shopspring/decimal"
)

type LiquidationBalances struct {
	LiquidatorAssetBalance     *Balance `json:"liquidatorAssetBalance"`
	LiquidatorLiabilityBalance *Balance `json:"liquidatorLiabilityBalance"`
	LiquidateeAssetBalance     *Balance `json:"liquidateeAssetBalance"`
	LiquidateeLiabilityBalance *Balance `json:"liquidateeLiabilityBalance"`
}

type LiquidateResult struct {
	PreBalances          *LiquidationBalances `json:"preBalances"`
	PostBalances         *LiquidationBalances `json:"postBalances"`
	LiquidateePreHealth  decimal.Decimal      `json:"liquidateePreHealth"`
	LiquidateePostHealth decimal.Decimal      `json:"liquidateePostHealth"`

	AssetBank     *Bank `json:"assetBank"`
	LiabilityBank *Bank `json:"liabilityBank"`

	LiquidatorAssetBalance     *BankAccountWrapper `json:"liquidatorAssetBalance"`
	LiquidatorLiabilityBalance *BankAccountWrapper `json:"liquidatorLiabilityBalance"`

	LiquidateeAssetBalance     *BankAccountWrapper `json:"liquidateeAssetBalance"`
	LiquidateeLiabilityBalance *BankAccountWrapper `json:"liquidateeLiabilityBalance"`
}
