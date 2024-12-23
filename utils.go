package core

import (
	"context"
	"slices"

	"github.com/gofrs/uuid"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type BankAccountService struct {
	BalanceStore
	BankStore
	AccountStore
}

type PriceAdapterMgr interface {
	GetPriceAdapter(bank *Bank) (PriceAdapter, error)
}

type PriceAdapter interface {
	GetPriceOfType(priceType OraclePriceType, bias PriceBias) (decimal.Decimal, error)
	GetAllPriceType() (price decimal.Decimal, priceLow decimal.Decimal, priceHigh decimal.Decimal, err error)
}

func ComputeLiquidationPriceForBank(bankAccountService BankAccountService, banks map[string]*Bank, changedbankAccounts []*BankAccountWrapper, priceFeedMgr PriceAdapterMgr, accountId, bankId uuid.UUID, marginReqType RequirementType) (decimal.Decimal, error) {
	var err error
	bank, ok := banks[bankId.String()]
	if !ok {
		return decimal.Zero, errors.Errorf("bank %s not found", bankId)
	}
	ctx := context.Background()
	var balance *Balance
	balance, err = bankAccountService.FindBalance(ctx, bankId, accountId)
	if err != nil && err != gorm.ErrRecordNotFound {
		return decimal.Zero, err
	}
	for _, ba := range changedbankAccounts {
		if ba.Balance.AccountId == accountId && ba.Balance.BankId == bankId {
			balance = ba.Balance
			break
		}
	}

	if balance == nil || !balance.Active {
		return decimal.Zero, nil
	}

	isLending := balance.LiabilityShares.IsZero()
	assets, liabilities, err := ComputeHealthComponents(bankAccountService, banks, priceFeedMgr, accountId, marginReqType, []uuid.UUID{bankId})
	if err != nil {
		return decimal.Zero, err
	}

	priceInfo, err := priceFeedMgr.GetPriceAdapter(bank)
	if err != nil {
		return decimal.Zero, err
	}
	price, err := priceInfo.GetPriceOfType(TimeWeighted, Original)
	if err != nil {
		return decimal.Zero, err
	}

	assetsQuantity, liabilitiesQuantity := balance.ComputeQuantity(bank)
	var liquidationPrice decimal.Decimal
	if isLending {
		if liabilities.IsZero() {
			return decimal.Zero, nil
		}
		if assetsQuantity.IsZero() {
			return decimal.Zero, nil
		}

		assetWeight := bank.GetAssetWeight(marginReqType, price, false)
		priceConfidence := bank.GetPrice(price, Original, false).Sub(bank.GetPrice(price, Low, false))
		denominator := assetsQuantity.Mul(assetWeight)
		if denominator.IsZero() {
			return decimal.Zero, nil
		}

		liquidationPrice = liabilities.
			Sub(assets.Div(denominator)).
			Add(priceConfidence)
	} else {
		if liabilitiesQuantity.IsZero() {
			return decimal.Zero, nil
		}

		liabWeight := bank.GetLiabilityWeight(marginReqType)
		priceConfidence := bank.GetPrice(price, High, false).Sub(bank.GetPrice(price, Original, false))
		denominator := liabilitiesQuantity.Mul(liabWeight)
		if denominator.IsZero() {
			return decimal.Zero, nil
		}
		liquidationPrice = assets.Sub(liabilities).Div(denominator).Sub(priceConfidence)
	}
	if liquidationPrice.IsZero() || liquidationPrice.LessThan(decimal.Zero) {
		return decimal.Zero, nil
	}
	return liquidationPrice, nil
}

func ComputeHealthComponents(bankAccountService BankAccountService, banks map[string]*Bank, priceFeedMgr PriceAdapterMgr, accountId uuid.UUID, marginReqType RequirementType, excludedBanks []uuid.UUID) (decimal.Decimal, decimal.Decimal, error) {
	ctx := context.Background()
	account, err := bankAccountService.GetAccountById(ctx, accountId)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}

	balances, err := bankAccountService.ListBalances(ctx, account.Id, uuid.Nil)
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}
	var filteredBalances []*Balance
	for _, balance := range balances {
		if slices.Contains(excludedBanks, balance.BankId) {
			continue
		}
		filteredBalances = append(filteredBalances, balance)
	}
	totalAssets := decimal.Zero
	totalLiabilities := decimal.Zero
	for _, balance := range filteredBalances {
		bank, ok := banks[balance.BankId.String()]
		if !ok {
			return decimal.Zero, decimal.Zero, errors.Errorf("bank %s not found", balance.BankId)
		}
		priceAdapter, err := priceFeedMgr.GetPriceAdapter(bank)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		price, err := priceAdapter.GetPriceOfType(TimeWeighted, Original)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		assets, liabilities := balance.GetUsdValueWithPriceBias(bank, price, marginReqType)
		totalAssets = totalAssets.Add(assets)
		totalLiabilities = totalLiabilities.Add(liabilities)
	}
	return totalAssets, totalLiabilities, nil
}

func CalcValue(amount decimal.Decimal, price decimal.Decimal, weight *decimal.Decimal) (decimal.Decimal, error) {
	if amount.IsZero() {
		return decimal.Zero, nil
	}

	var weighted_asset_amount decimal.Decimal
	if weight != nil {
		weighted_asset_amount = amount.Mul(*weight)
	} else {
		weighted_asset_amount = amount
	}

	value := weighted_asset_amount.Mul(price)
	return value, nil
}

func CalcAmount(value decimal.Decimal, price decimal.Decimal) (decimal.Decimal, error) {
	if price.IsZero() {
		return decimal.Zero, errors.New("price is zero")
	}
	return value.Div(price), nil
}

// calculate_pre_fee_spl_deposit_amount
func CalculatePreFeeSplDepositAmount(amount decimal.Decimal) (decimal.Decimal, error) {
	return amount, nil
}

// calculate_post_fee_spl_deposit_amount
func CalculatePostFeeSplDepositAmount(amount decimal.Decimal) (decimal.Decimal, error) {
	return amount, nil
}

// ComputeNetApy 
func ComputeNetApy(bankAccountService BankAccountService, priceFeedMgr PriceAdapterMgr, accountId uuid.UUID) (decimal.Decimal, error) {
	ctx := context.Background()
	account, err := bankAccountService.GetAccountById(ctx, accountId)
	if err != nil {
		return decimal.Zero, err
	}
	balances, err := bankAccountService.ListBalances(ctx, accountId, uuid.Nil)
	if err != nil {
		return decimal.Zero, err
	}
	var activeBankAccounts []*Balance
	for _, balance := range balances {
		if balance.Active {
			activeBankAccounts = append(activeBankAccounts, balance)
		}
	}

	riskEngine, err := NewRiskEngine(ctx, bankAccountService, account, []*BankAccountWrapper{}, priceFeedMgr)
	if err != nil {
		return decimal.Zero, err
	}
	totalAssets, totalLiabilities, err := riskEngine.GetAccountHealthComponents(Equity)
	if err != nil {
		return decimal.Zero, err
	}
	totalUsdValue := totalAssets.Sub(totalLiabilities)

	weightedApr := decimal.Zero
	for _, activeBalance := range activeBankAccounts {
		bank, err := bankAccountService.GetBankById(ctx, activeBalance.BankId)
		if err != nil {
			return decimal.Zero, err
		}

		priceAdapter, err := priceFeedMgr.GetPriceAdapter(bank)
		if err != nil {
			return decimal.Zero, err
		}

		priceInfo, err := priceAdapter.GetPriceOfType(RealTime, Original)
		if err != nil {
			return decimal.Zero, err
		}
		utilizationRatio := decimal.Zero
		if !totalAssets.IsZero() {
			utilizationRatio = totalLiabilities.Div(totalAssets)
		}
		lendingApr, borrowingApr, _, _, err := bank.BankConfig.InterestRateConfig.CalcInterestRate(utilizationRatio)
		if err != nil {
			return decimal.Zero, err
		}

		if totalUsdValue.IsZero() {
			totalUsdValue = ONE
		}

		assetUsdValue := activeBalance.AssetShares.Mul(priceInfo)
		assetApr := decimal.Zero
		if !totalUsdValue.IsZero() {
			assetApr = lendingApr.Mul(assetUsdValue).Div(totalUsdValue)
		}
		liabilityUsdValue := activeBalance.LiabilityShares.Mul(priceInfo)
		liabilityApr := decimal.Zero
		if !totalUsdValue.IsZero() {
			liabilityApr = borrowingApr.Mul(liabilityUsdValue).Div(totalUsdValue)
		}

		weightedApr = weightedApr.Add(assetApr).Sub(liabilityApr)
	}

	return AprToApy(weightedApr), nil
}

/*
const aprToApy = (apr: number, compoundingFrequency = HOURS_PER_YEAR) =>

	(1 + apr / compoundingFrequency) ** compoundingFrequency - 1;
*/
func AprToApy(apr decimal.Decimal) decimal.Decimal {
	hoursPerYear := decimal.NewFromInt(HOURS_PER_YEAR)
	if hoursPerYear.IsZero() {
		return decimal.Zero
	}
	return (ONE.Add(apr.Div(hoursPerYear))).Pow(hoursPerYear).Sub(ONE).Round(8)
}

func CalcInterestRateAccrualStateChanges(log Log, timeDelta uint64, totalAssetsAmount decimal.Decimal, totalLiabilitiesAmount decimal.Decimal, interestRateConfig InterestRateConfig, assetShareValue decimal.Decimal, liabilityShareValue decimal.Decimal) (decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	utilizationRate := totalLiabilitiesAmount.Div(totalAssetsAmount)

	lendingApr, borrowingApr, groupFeeApr, insuranceFeeApr, err := interestRateConfig.CalcInterestRate(utilizationRate)
	if err != nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, err
	}

	log.Info().Msgf("timeDelta: %d,utilizationRate: %s, lendingApr: %s, borrowingApr: %s, groupFeeApr: %s, insuranceFeeApr: %s", timeDelta, utilizationRate, lendingApr, borrowingApr, groupFeeApr, insuranceFeeApr)

	accruedAssetShareValue, err := CalcAccruedInterestPaymentPerPeriod(lendingApr, timeDelta, assetShareValue)
	if err != nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, err
	}
	accruedLiabilityShareValue, err := CalcAccruedInterestPaymentPerPeriod(borrowingApr, timeDelta, liabilityShareValue)
	if err != nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, err
	}

	groupFeePaymentForPeriod, err := CalcInterestPaymentForPeriod(groupFeeApr, timeDelta, totalLiabilitiesAmount)
	if err != nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, err
	}

	insuranceFeePaymentForPeriod, err := CalcInterestPaymentForPeriod(insuranceFeeApr, timeDelta, totalLiabilitiesAmount)
	if err != nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, err
	}

	return accruedAssetShareValue, accruedLiabilityShareValue, groupFeePaymentForPeriod, insuranceFeePaymentForPeriod, nil
}

func CalcAccruedInterestPaymentPerPeriod(apr decimal.Decimal, timeDelta uint64, value decimal.Decimal) (decimal.Decimal, error) {
	irPerPeriod := apr.Mul(decimal.NewFromInt(int64(timeDelta))).Div(decimal.NewFromInt(SECONDS_PER_YEAR))
	newValue := value.Mul(ONE.Add(irPerPeriod))
	return newValue, nil
}

func CalcInterestPaymentForPeriod(apr decimal.Decimal, timeDelta uint64, value decimal.Decimal) (decimal.Decimal, error) {
	interestPayment := value.Mul(apr).Mul(decimal.NewFromInt(int64(timeDelta))).Div(decimal.NewFromInt(SECONDS_PER_YEAR))
	return interestPayment, nil
}
