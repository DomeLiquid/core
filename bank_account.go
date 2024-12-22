package core

import (
	"context"

	"github.com/facebookgo/clock"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type (
	BankAccountWrapperStore interface {
		StorageBankAccount(ctx context.Context, bankAccount *BankAccountWrapper) error
		StorageLiquidationResult(ctx context.Context, bankAccount *LiquidateResult) error
	}

	BankAccountWrapper struct {
		clk clock.Clock `json:"-"`

		Balance *Balance `json:"balance"`
		Bank    *Bank    `json:"bank"`
	}
)

type OptionFunc func(ba *BankAccountWrapper)

func WithClock(clk clock.Clock) OptionFunc {
	return func(ba *BankAccountWrapper) {
		ba.clk = clk
	}
}

func NewBankAccountWrapper(balance *Balance, bank *Bank, opts ...OptionFunc) *BankAccountWrapper {
	ba := &BankAccountWrapper{
		Balance: balance,
		Bank:    bank,
		clk:     clock.New(),
	}
	for _, opt := range opts {
		opt(ba)
	}
	return ba
}

// only fill the balance
func FindBankAccountWrapper(ctx context.Context, bankAccountService BankAccountService, bank *Bank, account *Account, opts ...OptionFunc) (*BankAccountWrapper, error) {
	_, err := bankAccountService.GetBankById(ctx, bank.Id)
	if err != nil {
		return nil, BankAccountNotFound
	}

	balance, err := bankAccountService.FindBalance(ctx, bank.Id, account.Id)
	if err != nil {
		return nil, LendingAccountBalanceNotFound
	}

	return NewBankAccountWrapper(balance, bank, opts...), nil
}

func FindOrCreateBankAccountWrapper(ctx context.Context, clk clock.Clock, bankAccountService BankAccountService, bank *Bank, account *Account) (*BankAccountWrapper, error) {
	_, err := bankAccountService.GetBankById(ctx, bank.Id)
	if err != nil {
		return nil, BankAccountNotFound
	}

	balance, err := FindOrCreateBalance(ctx, clk, bankAccountService, bank, account)
	if err != nil {
		return nil, err
	}

	return NewBankAccountWrapper(balance, bank, WithClock(clk)), nil
}

func (ba *BankAccountWrapper) Deposit(log Log, amount decimal.Decimal) error {
	return ba.IncreaseBalanceInternal(log, amount, BalanceIncreaseTypeAny)
}

func (ba *BankAccountWrapper) Repay(log Log, amount decimal.Decimal) error {
	return ba.IncreaseBalanceInternal(log, amount, BalanceIncreaseTypeRepayOnly)
}

func (ba *BankAccountWrapper) Withdraw(log Log, amount decimal.Decimal) error {
	return ba.DecreaseBalanceInternal(log, amount, BalanceDecreaseTypeWithdrawOnly)
}

func (ba *BankAccountWrapper) Borrow(log Log, amount decimal.Decimal) error {
	return ba.DecreaseBalanceInternal(log, amount, BalanceDecreaseTypeAny)
}

// ------------ Hybrid operations for seamless repay + deposit / withdraw + borrow

func (ba *BankAccountWrapper) IncreaseBalance(log Log, amount decimal.Decimal) error {
	return ba.IncreaseBalanceInternal(log, amount, BalanceIncreaseTypeAny)
}

func (ba *BankAccountWrapper) IncreaseBalanceInLiquidation(log Log, amount decimal.Decimal) error {
	return ba.IncreaseBalanceInternal(log, amount, BalanceIncreaseTypeBypassDepositLimit)
}

func (ba *BankAccountWrapper) DecreaseBalanceInLiquidation(log Log, amount decimal.Decimal) error {
	return ba.DecreaseBalanceInternal(log, amount, BalanceDecreaseTypeBypassBorrowLimit)
}

func (ba *BankAccountWrapper) WithdrawAll(log Log) (decimal.Decimal, error) {
	currentTimestamp := ba.clk.Now().Unix()
	if err := ba.ClaimEmissions(log, currentTimestamp); err != nil {
		return decimal.Zero, err
	}

	balance := ba.Balance
	bank := ba.Bank

	if err := bank.AssertOperationalMode(false); err != nil {
		return decimal.Zero, err
	}

	totalAssetShares := balance.AssetShares
	totalLiabilityShares := balance.LiabilityShares

	currentLiabilityAmount, err := bank.GetLiabilityAmount(totalLiabilityShares)
	if err != nil {
		return decimal.Zero, err
	}

	if !currentLiabilityAmount.LessThan(EMPTY_BALANCE_THRESHOLD) {
		return decimal.Zero, NoAssetFound
	}

	currentAssetAmount, err := bank.GetAssetAmount(totalAssetShares)
	if err != nil {
		return decimal.Zero, err
	}

	log.Debug().Msgf("Withdrawing All: %s", currentAssetAmount)

	if !currentAssetAmount.GreaterThan(ZERO_AMOUNT_THRESHOLD) {
		return decimal.Zero, NoAssetFound
	}

	if err := balance.Close(ba.clk); err != nil {
		return decimal.Zero, err
	}

	bank.ChangeAssetShares(totalAssetShares.Mul(decimal.NewFromInt(-1)), false)

	if err := bank.CheckUtilizationRatio(); err != nil {
		return decimal.Zero, err
	}

	splWithdrawAmount := currentAssetAmount.Truncate(8)
	bank.CollectedInsuranceFeesOutstanding = bank.CollectedInsuranceFeesOutstanding.Add(currentAssetAmount.Sub(splWithdrawAmount))

	return splWithdrawAmount, nil
}

func (ba *BankAccountWrapper) RepayAll(log Log) (decimal.Decimal, error) {
	currentTimestamp := ba.clk.Now().Unix()
	ba.ClaimEmissions(log, currentTimestamp)

	balance := ba.Balance
	bank := ba.Bank

	if err := bank.AssertOperationalMode(false); err != nil {
		return decimal.Zero, err
	}

	totalAssetAmount := balance.AssetShares
	totalLiabilityAmount := balance.LiabilityShares

	currentLiabilityAmount, err := bank.GetLiabilityAmount(totalLiabilityAmount)
	if err != nil {
		return decimal.Zero, err
	}

	if !currentLiabilityAmount.GreaterThan(ZERO_AMOUNT_THRESHOLD) {
		return decimal.Zero, NoLiabilityFound
	}

	currentAssetAmount, err := bank.GetAssetAmount(totalAssetAmount)
	if err != nil {
		return decimal.Zero, err
	}

	if !currentAssetAmount.LessThan(EMPTY_BALANCE_THRESHOLD) {
		return decimal.Zero, NoAssetFound
	}

	if err := balance.Close(ba.clk); err != nil {
		return decimal.Zero, err
	}

	err = bank.ChangeLiabilityShares(totalLiabilityAmount.Mul(decimal.NewFromInt(-1)), false)
	if err != nil {
		return decimal.Zero, err
	}

	splDepositAmount := currentLiabilityAmount.RoundCeil(5)
	insuranceFeeIncrease := splDepositAmount.Sub(currentLiabilityAmount)
	bank.CollectedInsuranceFeesOutstanding = bank.CollectedInsuranceFeesOutstanding.Add(insuranceFeeIncrease)

	if bank.LiquidityVault.IsPositive() {
		bank.LiquidityVault = bank.LiquidityVault.Sub(insuranceFeeIncrease)
		bank.NormalizeLiquidityVault()
	}

	if bank.LiquidityVault.IsNegative() {
		return decimal.Zero, ErrBankLiquidityDeficit
	}

	return splDepositAmount, nil
}

func (ba *BankAccountWrapper) CloseBalance(log Log) error {
	currentTimestamp := ba.clk.Now().Unix()
	ba.ClaimEmissions(log, currentTimestamp)

	balance := ba.Balance
	bank := ba.Bank

	currentLiabilityAmount, err := bank.GetLiabilityAmount(balance.LiabilityShares)
	if err != nil {
		return err
	}
	currentAssetAmount, err := bank.GetAssetAmount(balance.AssetShares)
	if err != nil {
		return err
	}

	if !currentLiabilityAmount.LessThan(EMPTY_BALANCE_THRESHOLD) {
		log.Error().Msgf("Balance has existing debt")
		return IllegalBalanceState
	}

	if !currentAssetAmount.LessThan(EMPTY_BALANCE_THRESHOLD) {
		log.Error().Msgf("Balance has existing asset")
		return IllegalBalanceState
	}

	if err := balance.Close(ba.clk); err != nil {
		return err
	}

	return nil
}

func (ba *BankAccountWrapper) IncreaseBalanceInternal(log Log, balanceDelta decimal.Decimal, operationType BalanceIncreaseType) error {
	currentTimestamp := ba.clk.Now().Unix()
	ba.ClaimEmissions(log, currentTimestamp)

	balance := ba.Balance
	bank := ba.Bank

	currentLiabilityShares := balance.LiabilityShares
	currentLiabilityAmount, err := bank.GetLiabilityAmount(currentLiabilityShares)
	if err != nil {
		return err
	}
	liabilityAmountDecrease, assetAmountIncrease := decimal.Min(currentLiabilityAmount, balanceDelta), decimal.Max(balanceDelta.Sub(currentLiabilityAmount), decimal.Zero)

	switch operationType {
	case BalanceIncreaseTypeRepayOnly:
		if !assetAmountIncrease.IsZero() {
			return OperationRepayOnly
		}
	case BalanceIncreaseTypeDepositOnly:
		if !liabilityAmountDecrease.IsZero() {
			return OperationDepositOnly
		}
	default:
	}

	if err := bank.AssertOperationalMode(assetAmountIncrease.GreaterThan(ZERO_AMOUNT_THRESHOLD)); err != nil {
		return err
	}

	assetSharesIncrease, err := bank.GetAssetShares(assetAmountIncrease)
	if err != nil {
		return err
	}

	if err := balance.ChangeAssetShares(assetSharesIncrease); err != nil {
		return err
	}
	if err := bank.ChangeAssetShares(assetSharesIncrease, operationType == BalanceIncreaseTypeBypassDepositLimit); err != nil {
		return err
	}

	liabilitySharesDecrease, err := bank.GetLiabilityShares(liabilityAmountDecrease)
	if err != nil {
		return err
	}

	if err := balance.ChangeLiabilityShares(liabilitySharesDecrease.Mul(decimal.NewFromInt(-1))); err != nil {
		return err
	}
	if err := bank.ChangeLiabilityShares(liabilitySharesDecrease.Mul(decimal.NewFromInt(-1)), true); err != nil {
		return err
	}

	if err := bank.CheckUtilizationRatio(); err != nil {
		return err
	}

	return nil
}

func (ba *BankAccountWrapper) DecreaseBalanceInternal(log Log, balanceDelta decimal.Decimal, operationType BalanceDecreaseType) (err error) {
	log.Info().Msgf("Balance decrease: %s of (type: %s)", balanceDelta, operationType.String())
	currentTimestamp := ba.clk.Now().Unix()
	err = ba.ClaimEmissions(log, currentTimestamp)
	if err != nil {
		return err
	}

	balance := ba.Balance
	bank := ba.Bank

	currentAssetShares := balance.AssetShares
	currentAssetAmount, err := bank.GetAssetAmount(currentAssetShares)
	if err != nil {
		return err
	}

	assetAmountDecrease, liabilityAmountIncrease := decimal.Min(currentAssetAmount, balanceDelta), decimal.Max(balanceDelta.Sub(currentAssetAmount), decimal.Zero)
	switch operationType {
	case BalanceDecreaseTypeWithdrawOnly:
		if !liabilityAmountIncrease.IsZero() {
			return OperationWithdrawOnly
		}
	case BalanceDecreaseTypeBorrowOnly:
		if !assetAmountDecrease.IsZero() {
			return OperationBorrowOnly
		}
	default:
	}

	if err := bank.AssertOperationalMode(liabilityAmountIncrease.GreaterThan(ZERO_AMOUNT_THRESHOLD)); err != nil {
		return err
	}

	assetSharesDecrease, err := bank.GetAssetShares(assetAmountDecrease)
	if err != nil {
		return err
	}

	if err := balance.ChangeAssetShares(assetSharesDecrease.Mul(decimal.NewFromInt(-1))); err != nil {
		return err
	}
	if err := bank.ChangeAssetShares(assetSharesDecrease.Mul(decimal.NewFromInt(-1)), false); err != nil {
		return err
	}

	liabilitySharesIncrease, err := bank.GetLiabilityShares(liabilityAmountIncrease)
	if err != nil {
		return err
	}

	if err := balance.ChangeLiabilityShares(liabilitySharesIncrease); err != nil {
		return err
	}

	if err := bank.ChangeLiabilityShares(liabilitySharesIncrease, operationType == BalanceDecreaseTypeBypassBorrowLimit); err != nil {
		return err
	}

	if err := bank.CheckUtilizationRatio(); err != nil {
		return err
	}

	return nil
}

func (ba *BankAccountWrapper) ClaimEmissions(log Log, currentTimestamp int64) error {
	var balanceAmount decimal.Decimal

	side, err := ba.Balance.GetSide()
	if err != nil {
		return err
	}

	if side == BalanceSideAssets && ba.Bank.GetFlag(BankFlagsLendingActive) {
		amount, err := ba.Bank.GetAssetAmount(ba.Balance.AssetShares)
		if err != nil {
			return err
		}
		balanceAmount = amount
	} else if side == BalanceSideLiabilities && ba.Bank.GetFlag(BankFlagsBorrowActive) {
		amount, err := ba.Bank.GetLiabilityAmount(ba.Balance.LiabilityShares)
		if err != nil {
			return err
		}
		balanceAmount = amount
	} else {
		return nil
	}

	lastUpdate := ba.Balance.LastUpdate
	if lastUpdate < MIN_EMISSIONS_START_TIME {
		lastUpdate = currentTimestamp
	}

	period := currentTimestamp - lastUpdate
	if period <= 0 {
		return nil
	}

	emissionsRate := ba.Bank.EmissionsRate

	ba.Balance.LastUpdate = currentTimestamp

	emissions, err := CalcEmissions(period, balanceAmount, emissionsRate)
	if err != nil {
		return err
	}

	emissionsReal := decimal.Min(emissions, ba.Bank.EmissionsRemaining)

	if emissions.Cmp(emissionsReal) != 0 {
		log.Warn().Msgf("Emissions capped: %s (%s calculated) for period %ds", emissionsReal, emissions, period)
	}

	ba.Balance.EmissionsOutstanding = ba.Balance.EmissionsOutstanding.Add(emissionsReal)

	ba.Bank.EmissionsRemaining = ba.Bank.EmissionsRemaining.Sub(emissionsReal)

	return nil
}

func (ba *BankAccountWrapper) SettleEmissionsAndGetTransferAmount(log Log) decimal.Decimal {
	currentTimestamp := ba.clk.Now().Unix()
	ba.ClaimEmissions(log, currentTimestamp)
	emissionsOutstanding := ba.Balance.EmissionsOutstanding

	emissionsOutstandingFloored := emissionsOutstanding.Truncate(8)

	emissionsOutstanding = emissionsOutstanding.Sub(emissionsOutstandingFloored)
	ba.Balance.EmissionsOutstanding = emissionsOutstanding

	if ba.Balance.EmissionsOutstanding.GreaterThan(decimal.Zero) {
		ba.Bank.EmissionsRemaining = ba.Bank.EmissionsRemaining.Add(ba.Balance.EmissionsOutstanding)
		ba.Balance.EmissionsOutstanding = decimal.Zero
	}

	return emissionsOutstandingFloored
}

func (ba *BankAccountWrapper) WithdrawSplTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
	ba.Bank.WithdrawSplTransfer(amount, from, to)
}

func (ba *BankAccountWrapper) DepositSplTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
	ba.Bank.DepositSplTransfer(amount, from, to)
}

func CalcEmissions(period int64, balanceAmount decimal.Decimal, emissionsRate decimal.Decimal) (decimal.Decimal, error) {
	if period <= 0 {
		return decimal.Zero, nil
	}
	if emissionsRate.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, MathError
	}
	if balanceAmount.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero, MathError
	}

	return balanceAmount.Mul(emissionsRate).Mul(decimal.NewFromInt(period)).Div(decimal.NewFromInt(SECONDS_PER_YEAR)), nil
}

type BankAccountWithPriceFeed struct {
	Bank      *Bank
	Balance   *Balance
	PriceFeed PriceAdapter
}

func LoadBankAccountWithPriceFeeds(ctx context.Context, bankAccountService BankAccountService, accountId uuid.UUID, changedBankAccounts []*BankAccountWrapper, priceFeedMgr PriceAdapterMgr) ([]*BankAccountWithPriceFeed, error) {
	changedBankAccountsMap := make(map[uuid.UUID]*BankAccountWrapper)
	for _, bankAccount := range changedBankAccounts {
		changedBankAccountsMap[bankAccount.Bank.Id] = bankAccount
	}

	balances, err := bankAccountService.ListBalances(ctx, accountId, uuid.Nil)
	if err != nil {
		return nil, err
	}

	bankAccounts := make([]*BankAccountWithPriceFeed, 0, len(balances))

	if len(balances) == 0 {
		for _, bankAccount := range changedBankAccounts {
			priceFeed, err := priceFeedMgr.GetPriceAdapter(bankAccount.Bank)
			if err != nil {
				return nil, err
			}

			bankAccounts = append(bankAccounts, &BankAccountWithPriceFeed{
				Bank:      bankAccount.Bank,
				Balance:   bankAccount.Balance,
				PriceFeed: priceFeed,
			})
		}

		return bankAccounts, nil
	}

	for _, balance := range balances {
		bank, err := bankAccountService.GetBankById(ctx, balance.BankId)
		if err != nil {
			return nil, err
		}

		priceFeed, err := priceFeedMgr.GetPriceAdapter(bank)
		if err != nil {
			return nil, err
		}

		if bankAccount, ok := changedBankAccountsMap[balance.BankId]; ok && bankAccount != nil && bankAccount.Bank.Id == bank.Id {
			bankAccounts = append(bankAccounts, &BankAccountWithPriceFeed{
				Bank:      bankAccount.Bank,
				Balance:   bankAccount.Balance,
				PriceFeed: priceFeed,
			})
			continue
		}

		bankAccounts = append(bankAccounts, &BankAccountWithPriceFeed{
			Bank:      bank,
			Balance:   balance,
			PriceFeed: priceFeed,
		})
	}

	// Check for any changed bank accounts that weren't added yet
	existingBankIds := make(map[uuid.UUID]bool)
	for _, account := range bankAccounts {
		existingBankIds[account.Bank.Id] = true
	}

	for _, changedAccount := range changedBankAccounts {
		if !existingBankIds[changedAccount.Bank.Id] {
			priceFeed, err := priceFeedMgr.GetPriceAdapter(changedAccount.Bank)
			if err != nil {
				return nil, err
			}

			bankAccounts = append(bankAccounts, &BankAccountWithPriceFeed{
				Bank:      changedAccount.Bank,
				Balance:   changedAccount.Balance,
				PriceFeed: priceFeed,
			})
		}
	}

	return bankAccounts, nil
}

func (ba *BankAccountWithPriceFeed) CalcWeightedAssetsAndLiabsValues(requirementType RequirementType) (decimal.Decimal, decimal.Decimal, error) {
	side, err := ba.Balance.GetSide()
	if err != nil {
		return decimal.Zero, decimal.Zero, err
	}

	switch side {
	case BalanceSideAssets:
		assets, err := ba.CalcWeightedAssets(requirementType)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		return assets, decimal.Zero, nil
	case BalanceSideLiabilities:
		liabs, err := ba.CalcWeightedLiabs(requirementType)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		return decimal.Zero, liabs, nil
	}
	return decimal.Zero, decimal.Zero, nil
}

func (ba *BankAccountWithPriceFeed) CalcWeightedLiabs(requirementType RequirementType) (decimal.Decimal, error) {
	switch ba.Bank.BankConfig.RiskTier {
	case Collateral:
		priceFeed := ba.PriceFeed
		if priceFeed == nil {
			return decimal.Zero, nil
		}

		liabilityWeight := ba.Bank.BankConfig.GetWeight(requirementType, BalanceSideLiabilities)

		higherPrice, err := priceFeed.GetPriceOfType(requirementType.GetOraclePriceType(), High)
		if err != nil {
			return decimal.Zero, err
		}

		amount, err := ba.Bank.GetLiabilityAmount(ba.Balance.LiabilityShares)
		if err != nil {
			return decimal.Zero, err
		}

		return CalcValue(amount, higherPrice, &liabilityWeight)
	default:
		return decimal.Zero, nil
	}
}

func (ba *BankAccountWithPriceFeed) CalcWeightedAssets(requirementType RequirementType) (decimal.Decimal, error) {
	switch ba.Bank.BankConfig.RiskTier {
	case Collateral:
		priceFeed := ba.PriceFeed
		if priceFeed == nil {
			return decimal.Zero, nil
		}

		assetWeight := ba.Bank.BankConfig.GetWeight(requirementType, BalanceSideAssets)

		lowPrice, err := priceFeed.GetPriceOfType(requirementType.GetOraclePriceType(), Low)
		if err != nil {
			return decimal.Zero, err
		}

		if requirementType == Initial {
			discount, err := ba.Bank.MaybeGetAssetWeightInitDiscount(lowPrice)
			if err != nil {
				return decimal.Zero, err
			}
			if discount.GreaterThan(decimal.Zero) {
				assetWeight = assetWeight.Mul(discount)
			}
		}

		amount, err := ba.Bank.GetAssetAmount(ba.Balance.AssetShares)
		if err != nil {
			return decimal.Zero, err
		}

		weightedPrice, err := CalcValue(amount, lowPrice, &assetWeight)
		if err != nil {
			return decimal.Zero, err
		}

		return weightedPrice, nil
	default:
		return decimal.Zero, nil
	}
}

func (ba *BankAccountWithPriceFeed) IsEmpty(side BalanceSide) bool {
	return ba.Balance.IsEmpty(side)
}
