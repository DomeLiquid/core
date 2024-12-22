package core

import (
	"context"

	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type RiskEngine struct {
	MarginfiAccount       *Account
	BankAccountsWithPrice []*BankAccountWithPriceFeed
}

func NewRiskEngine(ctx context.Context, bankAccountService BankAccountService, account *Account, bankAccounts []*BankAccountWrapper, priceFeedMgr PriceAdapterMgr) (*RiskEngine, error) {
	if account.GetFlag(InFlashloanFlag) {
		return nil, AccountInFlashloan
	}

	return NewRiskEngineNoFlashloanCheck(ctx, bankAccountService, account, bankAccounts, priceFeedMgr)
}

func NewRiskEngineNoFlashloanCheck(ctx context.Context, bankAccountService BankAccountService, account *Account, bankAccounts []*BankAccountWrapper, priceFeedMgr PriceAdapterMgr) (*RiskEngine, error) {
	bankAccountsWithPrice, err := LoadBankAccountWithPriceFeeds(ctx, bankAccountService, account.Id, bankAccounts, priceFeedMgr)
	if err != nil {
		return nil, err
	}
	return &RiskEngine{
		MarginfiAccount:       account,
		BankAccountsWithPrice: bankAccountsWithPrice,
	}, nil
}

func (r *RiskEngine) CheckAccountInitHealth(ctx context.Context, bankAccountService BankAccountService, account *Account, bankAccounts []*BankAccountWrapper, priceFeedMgr PriceAdapterMgr) error {
	if account.GetFlag(InFlashloanFlag) {
		return nil
	}

	noFlashloanCheck, err := NewRiskEngineNoFlashloanCheck(ctx, bankAccountService, r.MarginfiAccount, bankAccounts, priceFeedMgr)
	if err != nil {
		return err
	}

	return noFlashloanCheck.CheckAccountHealth(Initial)
}

func (r *RiskEngine) GetAccountHealthComponents(requirementType RequirementType) (decimal.Decimal, decimal.Decimal, error) {
	totalAssets := decimal.Zero
	totalLiabilities := decimal.Zero
	for _, a := range r.BankAccountsWithPrice {
		assets, liabilities, err := a.CalcWeightedAssetsAndLiabsValues(requirementType)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		totalAssets = totalAssets.Add(assets)
		totalLiabilities = totalLiabilities.Add(liabilities)
	}
	return totalAssets, totalLiabilities, nil
}

func (r *RiskEngine) GetAccountHealth(requirementType RequirementType) (decimal.Decimal, error) {
	totalAssets, totalLiabilities, err := r.GetAccountHealthComponents(requirementType)
	if err != nil {
		return decimal.Zero, err
	}
	return totalAssets.Sub(totalLiabilities), nil
}

func (r *RiskEngine) CheckAccountHealth(requirementType RequirementType) error {
	totalAssets, totalLiabilities, err := r.GetAccountHealthComponents(requirementType)
	if err != nil {
		return err
	}
	if !totalAssets.GreaterThanOrEqual(totalLiabilities) {
		return RiskEngineInitRejected
	}
	err = r.CheckAccountRiskTiers()
	if err != nil {
		return err
	}
	return nil
}

func (r *RiskEngine) CheckPreLiquidationConditionAndGetAccountHealth(bankId uuid.UUID) (decimal.Decimal, error) {
	if r.MarginfiAccount.GetFlag(InFlashloanFlag) {
		return decimal.Zero, AccountInFlashloan
	}
	var liabilityBankBalance *BankAccountWithPriceFeed
	bankAccountsWithPrices := r.BankAccountsWithPrice
	for _, a := range bankAccountsWithPrices {
		if a.Balance.BankId == bankId {
			liabilityBankBalance = a
		}
	}

	if liabilityBankBalance == nil {
		return decimal.Zero, LendingAccountBalanceNotFound
	}
	if liabilityBankBalance.IsEmpty(BalanceSideLiabilities) {
		return decimal.Zero, IllegalLiquidation
	}
	if !liabilityBankBalance.IsEmpty(BalanceSideAssets) {
		return decimal.Zero, IllegalLiquidation
	}

	totalAssets, totalLiabilities, err := r.GetAccountHealthComponents(Maintenance)
	if err != nil {
		return decimal.Zero, err
	}

	accountHealth := totalAssets.Sub(totalLiabilities)
	if !accountHealth.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, ErrAccountNotUnhealthy
	}
	return accountHealth, nil
}

/*
Checks whether the account meets the maintenance requirement level after liquidation.
This check ensures the following during the liquidation process:
1. We verify that the remaining liabilities of the liquidated account are not empty.
2. The liquidated account was below the maintenance requirement level before liquidation (since health can only increase, as liquidation always repays some liabilities).
3. The liquidator does not liquidate too many assets, causing unnecessary losses to the liquidated account.

This check is based on the assumption that liquidation always reduces risk.

1. We check that the repaid liabilities are not zero. This ensures that liquidation does not exceed necessary limits.
2. We ensure that the account remains at or below the maintenance requirement level. This ensures that the overall liquidation is not excessive.
*/
func (r *RiskEngine) CheckPostLiquidationConditionAndGetAccountHealth(bankId uuid.UUID, preLiquidationHealth decimal.Decimal) (decimal.Decimal, error) {
	if r.MarginfiAccount.GetFlag(InFlashloanFlag) {
		return decimal.Zero, AccountInFlashloan
	}

	var liabilityBankBalance *BankAccountWithPriceFeed
	bankAccountsWithPrices := r.BankAccountsWithPrice
	for _, a := range bankAccountsWithPrices {
		if a.Balance.BankId == bankId {
			liabilityBankBalance = a
		}
	}

	if liabilityBankBalance.IsEmpty(BalanceSideLiabilities) {
		return decimal.Zero, IllegalLiquidation
	}

	if !liabilityBankBalance.IsEmpty(BalanceSideAssets) {
		return decimal.Zero, IllegalLiquidation
	}

	totalAssets, totalLiabilities, err := r.GetAccountHealthComponents(Maintenance)
	if err != nil {
		return decimal.Zero, err
	}

	accountHealth := totalAssets.Sub(totalLiabilities)
	if !accountHealth.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, ErrAccountNotUnhealthy
	}

	if accountHealth.LessThanOrEqual(preLiquidationHealth) {
		return decimal.Zero, IllegalLiquidation
	}

	return accountHealth, nil
}

func (r *RiskEngine) CheckAccountBankrupt(log Log) error {
	if r.MarginfiAccount.GetFlag(InFlashloanFlag) {
		return AccountInFlashloan
	}

	totalAssets, totalLiabilities, err := r.GetAccountHealthComponents(Equity)
	if err != nil {
		return err
	}

	log.Debug().Msgf("totalAssets: %s, totalLiabilities: %s", totalAssets, totalLiabilities)

	if !totalAssets.LessThan(totalLiabilities) {
		return AccountNotBankrupt
	}

	// TODO
	if totalAssets.LessThan(BANKRUPT_THRESHOLD) {
		return AccountNotBankrupt
	}

	if totalLiabilities.LessThan(ZERO_AMOUNT_THRESHOLD) {
		return AccountNotBankrupt
	}

	return nil
}

func (r *RiskEngine) CheckAccountRiskTiers() error {
	balancesWithLiablities := []*BankAccountWithPriceFeed{}
	for _, a := range r.BankAccountsWithPrice {
		if !a.Balance.IsEmpty(BalanceSideLiabilities) {
			balancesWithLiablities = append(balancesWithLiablities, a)
		}
	}
	nBalancesWithLiablities := len(balancesWithLiablities)

	isInIsolatedRiskTier := false
	for _, a := range balancesWithLiablities {
		if a.Bank.BankConfig.RiskTier == Isolated {
			isInIsolatedRiskTier = true
		}
	}
	if isInIsolatedRiskTier && nBalancesWithLiablities != 1 {
		return IsolatedAccountIllegalState
	}
	return nil
}
