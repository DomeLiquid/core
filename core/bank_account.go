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

	// 检查银行是否满足流动性要求
	if err := bank.CheckUtilizationRatio(); err != nil {
		return decimal.Zero, err
	}

	splWithdrawAmount := currentAssetAmount.Truncate(8)
	bank.CollectedInsuranceFeesOutstanding = bank.CollectedInsuranceFeesOutstanding.Add(currentAssetAmount.Sub(splWithdrawAmount))

	return splWithdrawAmount, nil
}

// RepayAll 还清所有负债
func (ba *BankAccountWrapper) RepayAll(log Log) (decimal.Decimal, error) {
	// 领取当前时间的奖励
	currentTimestamp := ba.clk.Now().Unix()
	ba.ClaimEmissions(log, currentTimestamp)

	balance := ba.Balance
	bank := ba.Bank

	// 确保银行处于非操作模式
	if err := bank.AssertOperationalMode(false); err != nil {
		return decimal.Zero, err
	}

	// 获取账户的总资产和总负债
	totalAssetAmount := balance.AssetShares
	totalLiabilityAmount := balance.LiabilityShares

	// 获取当前的负债金额
	currentLiabilityAmount, err := bank.GetLiabilityAmount(totalLiabilityAmount)
	if err != nil {
		return decimal.Zero, err
	}

	// 如果当前负债金额小于阈值，返回没有找到负债的错误
	if !currentLiabilityAmount.GreaterThan(ZERO_AMOUNT_THRESHOLD) {
		return decimal.Zero, NoLiabilityFound
	}

	// 获取当前的资产金额
	currentAssetAmount, err := bank.GetAssetAmount(totalAssetAmount)
	if err != nil {
		return decimal.Zero, err
	}

	// 如果当前资产金额小于阈值，返回没有找到资产的错误
	if !currentAssetAmount.LessThan(EMPTY_BALANCE_THRESHOLD) {
		return decimal.Zero, NoAssetFound
	}

	// 关闭账户余额
	if err := balance.Close(ba.clk); err != nil {
		return decimal.Zero, err
	}

	// 减少银行的负债份额
	err = bank.ChangeLiabilityShares(totalLiabilityAmount.Mul(decimal.NewFromInt(-1)), false)
	if err != nil {
		return decimal.Zero, err
	}

	// 计算应存入的金额，并更新银行的保险费
	// 向上取整到5位小数
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

	// 返回应存入的金额
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
	// 根据操作类型进行不同的处理
	switch operationType {
	// 如果操作类型是仅提款
	case BalanceDecreaseTypeWithdrawOnly:
		// 检查负债增加量是否小于空余额阈值
		if !liabilityAmountIncrease.IsZero() {
			// 如果是，返回没有找到负债的错误
			return OperationWithdrawOnly
		}
	// 如果操作类型是仅借款
	case BalanceDecreaseTypeBorrowOnly:
		// 检查资产减少量是否小于空余额阈值
		if !assetAmountDecrease.IsZero() {
			// 如果是，返回没有找到资产的错误
			return OperationBorrowOnly
		}
	// 默认情况下，不做任何处理
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

// ClaimEmissions 领取任何未领取的排放量，并将其添加到未结排放量中
func (ba *BankAccountWrapper) ClaimEmissions(log Log, currentTimestamp int64) error {
	// 根据账户的资产或负债状态以及银行的排放标志，确定是否有未领取的排放量
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

	// 确定上次更新的时间，如果上次更新时间小于最小排放开始时间，则使用当前时间
	lastUpdate := ba.Balance.LastUpdate
	if lastUpdate < MIN_EMISSIONS_START_TIME {
		lastUpdate = currentTimestamp
	}

	// 计算排放周期
	period := currentTimestamp - lastUpdate
	if period <= 0 {
		return nil
	}

	// 获取银行的排放率
	emissionsRate := ba.Bank.EmissionsRate

	// 更新账户的最后更新时间
	ba.Balance.LastUpdate = currentTimestamp

	// 计算排放量
	emissions, err := CalcEmissions(period, balanceAmount, emissionsRate)
	if err != nil {
		return err
	}

	// 确定实际排放量，不能超过银行剩余的排放量
	emissionsReal := decimal.Min(emissions, ba.Bank.EmissionsRemaining)

	// 如果计算的排放量超过实际排放量，记录日志
	if emissions.Cmp(emissionsReal) != 0 {
		log.Warn().Msgf("Emissions capped: %s (%s calculated) for period %ds", emissionsReal, emissions, period)
	}

	// 更新账户的未结排放量
	ba.Balance.EmissionsOutstanding = ba.Balance.EmissionsOutstanding.Add(emissionsReal)

	// 更新银行的剩余排放量
	ba.Bank.EmissionsRemaining = ba.Bank.EmissionsRemaining.Sub(emissionsReal)

	return nil
}

// 结算所有未领取的排放量，并返回可以提取的最大金额。
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

// ------------ SPL helpers
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

// 计算加权资产和负债的值
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

// 计算负债的加权价值
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

// 计算加权资产
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

// 判断账户是否为空
func (ba *BankAccountWithPriceFeed) IsEmpty(side BalanceSide) bool {
	return ba.Balance.IsEmpty(side)
}

// func (ba *BankAccountWithPriceFeed) DepositTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
// 	ba.Bank.DepositTransfer(amount, from, to)
// }

// func (ba *BankAccountWithPriceFeed) WithdrawTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
// 	ba.Bank.WithdrawTransfer(amount, from, to)
// }
