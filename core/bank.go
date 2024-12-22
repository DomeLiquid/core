package core

import (
	"context"
	"math"
	"time"

	"github.com/DomeLiquid/core/utils"
	"github.com/facebookgo/clock"
	"github.com/gofrs/uuid"
	"github.com/shopspring/decimal"
)

type (
	BankStore interface {
		CreateBank(ctx context.Context, bank *Bank) error
		UpsertBank(ctx context.Context, bank *Bank) error
		ListBank(ctx context.Context) ([]*Bank, error)
		GetBankById(ctx context.Context, bankId uuid.UUID) (*Bank, error)
		ListBankByGroupId(ctx context.Context, groupId uuid.UUID) ([]*Bank, error)
		GetBanksByGroupId(ctx context.Context, groupId uuid.UUID) ([]*Bank, error)
		GetBankByName(ctx context.Context, bankName string) (*Bank, error)
		GetBankByMixinSafeAssetId(ctx context.Context, mixinSafeAssetId string) (*Bank, error)
		UpdateBankConfig(ctx context.Context, bankId uuid.UUID, bankConfig *BankConfig) error
		UpdateBank(ctx context.Context, bankId uuid.UUID, bank *Bank) error
	}

	Bank struct {
		Id      uuid.UUID `json:"id"`
		GroupId uuid.UUID `json:"groupId"`
		Name    string    `json:"name"`

		MixinSafeAssetId string `json:"mixinSafeAssetId"`

		AssetShareValue     decimal.Decimal `json:"assetShareValue"`
		LiabilityShareValue decimal.Decimal `json:"liabilityShareValue"`

		LiquidityVault decimal.Decimal `json:"liquidityVault"`
		InsuranceVault decimal.Decimal `json:"insuranceVault"`
		FeeVault       decimal.Decimal `json:"feeVault"`

		CollectedInsuranceFeesOutstanding decimal.Decimal `json:"collectedInsuranceFeesOutstanding"`
		CollectedGroupFeesOutstanding     decimal.Decimal `json:"collectedGroupFeesOutstanding"`

		TotalLiabilityShares decimal.Decimal `json:"totalLiabilityShares"`
		TotalAssetShares     decimal.Decimal `json:"totalAssetShares"`

		Flags BankFlags `json:"flags"`

		BankConfig `json:"bankConfig"`

		EmissionsMixinSafeAssetId string          `json:"emissionsMixinSafeAssetId"`
		EmissionsRate             decimal.Decimal `json:"emissionsRate"`
		EmissionsRemaining        decimal.Decimal `json:"emissionsRemaining"`

		CreatedAt  int64 `json:"createdAt"`
		LastUpdate int64 `json:"lastUpdate"`

		DeletedAt int64 `json:"deletedAt"`
	}

	BankConfig struct {
		AssetWeightInit  decimal.Decimal `json:"assetWeightInit"`
		AssetWeightMaint decimal.Decimal `json:"assetWeightMaint"`

		LiabilityWeightInit  decimal.Decimal `json:"liabilityWeightInit"`
		LiabilityWeightMaint decimal.Decimal `json:"liabilityWeightMaint"`

		DepositLimit   decimal.Decimal `json:"depositLimit"`
		LiabilityLimit decimal.Decimal `json:"liabilityLimit"`

		InterestRateConfig `json:"interestRateConfig"`

		OperationalState BankOperationalState `json:"operationalState"`

		RiskTier                 RiskTier        `json:"riskTier"`
		TotalAssetValueInitLimit decimal.Decimal `json:"totalAssetValueInitLimit"`

		OracleSetup  OracleSetup `json:"oracleSetup"`
		OracleMaxAge int64       `json:"oracleMaxAge"`
	}

	InterestRateConfig struct {
		OptimalUtilizationRate decimal.Decimal `json:"optimalUtilizationRate"`
		PlateauInterestRate    decimal.Decimal `json:"plateauInterestRate"`
		MaxInterestRate        decimal.Decimal `json:"maxInterestRate"`

		InsuranceFeeFixedApr decimal.Decimal `json:"insuranceFeeFixedApr"`
		InsuranceIrFee       decimal.Decimal `json:"insuranceIrFee"`
		ProtocolFixedFeeApr  decimal.Decimal `json:"protocolFixedFeeApr"`
		ProtocolIrFee        decimal.Decimal `json:"protocolIrFee"`
	}
)

func (i *InterestRateConfig) CalcInterestRate(utilizationRatio decimal.Decimal) (decimal.Decimal, decimal.Decimal, decimal.Decimal, decimal.Decimal, error) {
	protocolIrFee := i.ProtocolIrFee
	insuranceIrFee := i.InsuranceIrFee
	protocolFixedFeeApr := i.ProtocolFixedFeeApr
	insuranceFeeFixedApr := i.InsuranceFeeFixedApr

	rateFee := protocolIrFee.Add(insuranceIrFee)
	totalFixedFeeApr := protocolFixedFeeApr.Add(insuranceFeeFixedApr)

	baseRate := i.InterestRateCurve(utilizationRatio)

	lendingRate := baseRate.Mul(utilizationRatio)
	borrowingRate := baseRate.Mul(ONE.Add(rateFee)).Add(totalFixedFeeApr)

	groupFeesApr := i.CalcFeeRate(baseRate, protocolIrFee, protocolFixedFeeApr)
	insuranceFeesApr := i.CalcFeeRate(baseRate, insuranceIrFee, insuranceFeeFixedApr)

	if lendingRate.LessThan(decimal.Zero) ||
		borrowingRate.LessThan(decimal.Zero) ||
		groupFeesApr.LessThan(decimal.Zero) ||
		insuranceFeesApr.LessThan(decimal.Zero) {
		return decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero, ErrNegativeInterestRate
	}

	return lendingRate, borrowingRate, groupFeesApr, insuranceFeesApr, nil
}

func (i *InterestRateConfig) InterestRateCurve(utilizationRatio decimal.Decimal) decimal.Decimal {
	optimalUr := i.OptimalUtilizationRate
	plateauIr := i.PlateauInterestRate
	maxIr := i.MaxInterestRate

	if utilizationRatio.LessThanOrEqual(optimalUr) {
		// ur / optimal_ur * plateau_ir
		return utilizationRatio.Mul(plateauIr).Div(optimalUr)
	} else {
		// (ur - optimal_ur) / (1 - optimal_ur) * (max_ir - plateau_ir) + plateau_ir
		oneMinusOptimalUr := ONE.Sub(optimalUr)
		maxIrMinusPlateau := maxIr.Sub(plateauIr)
		utilizationRatioMinusOptimalUr := utilizationRatio.Sub(optimalUr)

		result := utilizationRatioMinusOptimalUr.Div(oneMinusOptimalUr).Mul(maxIrMinusPlateau).Add(plateauIr)
		return result
	}
}

func (i *InterestRateConfig) CalcFeeRate(baseRate, irFee, fixedFeeApr decimal.Decimal) decimal.Decimal {
	return baseRate.Mul(irFee).Add(fixedFeeApr)
}

func (i *InterestRateConfig) Validate() error {
	optimalUr := i.OptimalUtilizationRate
	plateauIr := i.PlateauInterestRate
	maxIr := i.MaxInterestRate

	if optimalUr.LessThanOrEqual(decimal.Zero) || optimalUr.GreaterThanOrEqual(ONE) {
		return ErrOptimalUr
	}
	if plateauIr.LessThanOrEqual(decimal.Zero) {
		return ErrPlateauIr
	}
	if maxIr.LessThanOrEqual(decimal.Zero) {
		return ErrMaxIr
	}
	if plateauIr.GreaterThanOrEqual(maxIr) {
		return ErrPlateauGreaterThanMax
	}

	return nil
}

func (i *InterestRateConfig) Update(irConfig *InterestRateConfig) {
	if !irConfig.OptimalUtilizationRate.IsZero() {
		i.OptimalUtilizationRate = irConfig.OptimalUtilizationRate
	}
	if !irConfig.PlateauInterestRate.IsZero() {
		i.PlateauInterestRate = irConfig.PlateauInterestRate
	}
	if !irConfig.MaxInterestRate.IsZero() {
		i.MaxInterestRate = irConfig.MaxInterestRate
	}
	if !irConfig.InsuranceFeeFixedApr.IsZero() {
		i.InsuranceFeeFixedApr = irConfig.InsuranceFeeFixedApr
	}
	if !irConfig.InsuranceIrFee.IsZero() {
		i.InsuranceIrFee = irConfig.InsuranceIrFee
	}
	if !irConfig.ProtocolFixedFeeApr.IsZero() {
		i.ProtocolFixedFeeApr = irConfig.ProtocolFixedFeeApr
	}
	if !irConfig.ProtocolIrFee.IsZero() {
		i.ProtocolIrFee = irConfig.ProtocolIrFee
	}
}

type BankOperationalState uint8

func (bos BankOperationalState) String() string {
	switch bos {
	case BankOperationalStatePaused:
		return "Paused"
	case BankOperationalStateOperational:
		return "Operational"
	case BankOperationalStateReduceOnly:
		return "Reduce Only"
	case BankOperationalStateNone:
		return "None"
	default:
		return "Unknown"
	}
}

const (
	BankOperationalStatePaused BankOperationalState = iota
	BankOperationalStateOperational
	BankOperationalStateReduceOnly
	BankOperationalStateNone
)

type RiskTier uint8

const (
	Collateral RiskTier = iota
	Isolated
)

type BankFlags uint8

const (
	BankFlagsBorrowActive                    BankFlags = 1 << 0
	BankFlagsLendingActive                   BankFlags = 1 << 1
	BankFlagsPermissionlessBadDebtSettlement BankFlags = 1 << 2

	BankFlagsEmissionsActive BankFlags = BankFlagsBorrowActive | BankFlagsLendingActive
	BankFlagsGroupActive     BankFlags = BankFlagsPermissionlessBadDebtSettlement | BankFlagsEmissionsActive
)

func (bf BankFlags) String() string {
	switch bf {
	case BankFlagsBorrowActive:
		return "Borrow Active"
	case BankFlagsLendingActive:
		return "Lending Active"
	case BankFlagsPermissionlessBadDebtSettlement:
		return "Permissionless Bad Debt Settlement"
	case BankFlagsEmissionsActive:
		return "Emissions Active"
	case BankFlagsGroupActive:
		return "Group Active"
	default:
		return "Unknown"
	}
}

type BalanceSide uint8

const (
	BalanceSideAssets BalanceSide = iota
	BalanceSideLiabilities
	BalanceSideEmpty
)

func (bs BalanceSide) String() string {
	switch bs {
	case BalanceSideAssets:
		return "Assets"
	case BalanceSideLiabilities:
		return "Liabilities"
	case BalanceSideEmpty:
		return "Empty"
	default:
		return "Unknown"
	}
}

func ValidateBankConfig(bankConfig *BankConfig) error {
	oracleAis := bankConfig.OracleSetup
	oracleMaxAge := bankConfig.OracleMaxAge

	switch oracleAis {
	case MixinOracle:
		if oracleMaxAge > 90 {
			return ErrOracleMaxAgeTooLong
		}
	default:
		return ErrUnknownOracleSetup
	}
	return nil
}

func (bc *BankConfig) GetWeights(requirementType RequirementType) (decimal.Decimal, decimal.Decimal) {
	switch requirementType {
	case Initial:
		return bc.AssetWeightInit, bc.LiabilityWeightInit
	case Maintenance:
		return bc.AssetWeightMaint, bc.LiabilityWeightMaint
	case Equity:
		return ONE, ONE
	default:
		return decimal.Zero, decimal.Zero
	}
}

func (bc *BankConfig) GetWeight(requirementType RequirementType, balanceSide BalanceSide) decimal.Decimal {
	switch {
	case requirementType == Initial && balanceSide == BalanceSideAssets:
		return bc.AssetWeightInit
	case requirementType == Initial && balanceSide == BalanceSideLiabilities:
		return bc.LiabilityWeightInit
	case requirementType == Maintenance && balanceSide == BalanceSideAssets:
		return bc.AssetWeightMaint
	case requirementType == Maintenance && balanceSide == BalanceSideLiabilities:
		return bc.LiabilityWeightMaint
	case requirementType == Equity:
		return ONE
	default:
		return decimal.Zero
	}
}

func (bc *BankConfig) Validate() error {
	assetInitW := bc.AssetWeightInit
	assetMaintW := bc.AssetWeightMaint

	if !(assetInitW.GreaterThanOrEqual(decimal.Zero) && assetInitW.LessThanOrEqual(ONE)) {
		return InvalidConfig
	}

	if !(assetMaintW.GreaterThanOrEqual(assetInitW)) {
		return InvalidConfig
	}

	liabInitW := bc.LiabilityWeightInit
	liabMaintW := bc.LiabilityWeightMaint
	if liabInitW.LessThan(ONE) {
		return InvalidConfig
	}

	if liabMaintW.GreaterThan(liabInitW) || liabMaintW.LessThan(ONE) {
		return InvalidConfig
	}

	if err := bc.InterestRateConfig.Validate(); err != nil {
		return err
	}

	if bc.RiskTier == Isolated {
		if !assetInitW.Equal(decimal.Zero) {
			return InvalidConfig
		}
		if !assetMaintW.Equal(decimal.Zero) {
			return InvalidConfig
		}
	}

	return nil
}

func (bc *BankConfig) IsDepositLimitActive() bool {
	return !bc.DepositLimit.Equal(decimal.NewFromUint64(math.MaxUint64))
}

func (bc *BankConfig) IsBorrowLimitActive() bool {
	return !bc.LiabilityLimit.Equal(decimal.NewFromUint64(math.MaxUint64))
}

func (bc *BankConfig) UsdInitLimitActive() bool {
	return !bc.TotalAssetValueInitLimit.Equal(decimal.NewFromUint64(math.MaxUint64))
}

func NewBank(clk clock.Clock, groupId uuid.UUID, name string, mixinSafeAssetId string, bankConfig BankConfig) *Bank {
	return NewBankWithCreateTime(clk, groupId, name, mixinSafeAssetId, bankConfig, clk.Now())
}

func NewBankWithCreateTime(clk clock.Clock, groupId uuid.UUID, name string, mixinSafeAssetId string, bankConfig BankConfig, createTime time.Time) *Bank {
	return &Bank{
		Id:                                uuid.Must(uuid.FromString(utils.GenUuidFromStrings(groupId.String(), name, mixinSafeAssetId))),
		GroupId:                           groupId,
		Name:                              name,
		MixinSafeAssetId:                  mixinSafeAssetId,
		AssetShareValue:                   ONE,
		LiabilityShareValue:               ONE,
		LiquidityVault:                    decimal.Zero,
		InsuranceVault:                    decimal.Zero,
		FeeVault:                          decimal.Zero,
		CollectedInsuranceFeesOutstanding: decimal.Zero,
		CollectedGroupFeesOutstanding:     decimal.Zero,
		TotalLiabilityShares:              decimal.Zero,
		TotalAssetShares:                  decimal.Zero,
		Flags:                             BankFlags(0),
		BankConfig:                        bankConfig,
		CreatedAt:                         createTime.Unix(),
		LastUpdate:                        createTime.Unix(),
	}
}

func (b *Bank) Clone() *Bank {
	return &Bank{
		Id:                                b.Id,
		GroupId:                           b.GroupId,
		Name:                              b.Name,
		MixinSafeAssetId:                  b.MixinSafeAssetId,
		AssetShareValue:                   b.AssetShareValue,
		LiabilityShareValue:               b.LiabilityShareValue,
		LiquidityVault:                    b.LiquidityVault,
		InsuranceVault:                    b.InsuranceVault,
		FeeVault:                          b.FeeVault,
		CollectedInsuranceFeesOutstanding: b.CollectedInsuranceFeesOutstanding,
		CollectedGroupFeesOutstanding:     b.CollectedGroupFeesOutstanding,
		TotalLiabilityShares:              b.TotalLiabilityShares,
		TotalAssetShares:                  b.TotalAssetShares,
		Flags:                             b.Flags,
		BankConfig:                        b.BankConfig,
		EmissionsMixinSafeAssetId:         b.EmissionsMixinSafeAssetId,
		EmissionsRate:                     b.EmissionsRate,
		EmissionsRemaining:                b.EmissionsRemaining,
		CreatedAt:                         b.CreatedAt,
		LastUpdate:                        b.LastUpdate,
	}
}

func (b *Bank) GetFlag(flag BankFlags) bool {
	return b.Flags&flag == flag
}

func (b *Bank) OverrideEmissionsFlag(flag BankFlags) {
	b.Flags = flag
}

func (b *Bank) UpdateFlag(value bool, flag BankFlags) {
	if value {
		b.Flags |= flag
	} else {
		b.Flags &= ^flag
	}
}

func (b *Bank) VerifyEmissionsFlags(flags BankFlags) bool {
	return flags&BankFlagsEmissionsActive == flags
}

func (b *Bank) VerifyGroupFlags(flags BankFlags) bool {
	return flags&BankFlagsGroupActive == flags
}

func (b *Bank) Configure(config *BankConfig) error {
	if !config.AssetWeightInit.IsZero() {
		b.BankConfig.AssetWeightInit = config.AssetWeightInit
	}
	if !config.AssetWeightMaint.IsZero() {
		b.BankConfig.AssetWeightMaint = config.AssetWeightMaint
	}
	if !config.LiabilityWeightInit.IsZero() {
		b.BankConfig.LiabilityWeightInit = config.LiabilityWeightInit
	}
	if !config.LiabilityWeightMaint.IsZero() {
		b.BankConfig.LiabilityWeightMaint = config.LiabilityWeightMaint
	}
	if !config.DepositLimit.IsZero() {
		b.BankConfig.DepositLimit = config.DepositLimit
	}
	if !config.LiabilityLimit.IsZero() {
		b.BankConfig.LiabilityLimit = config.LiabilityLimit
	}
	if config.InterestRateConfig != (InterestRateConfig{}) {
		b.BankConfig.InterestRateConfig = config.InterestRateConfig
	}
	if config.RiskTier != 0 {
		b.BankConfig.RiskTier = config.RiskTier
	}
	if !config.TotalAssetValueInitLimit.IsZero() {
		b.BankConfig.TotalAssetValueInitLimit = config.TotalAssetValueInitLimit
	}
	if config.OracleMaxAge != 0 {
		b.BankConfig.OracleMaxAge = config.OracleMaxAge
	}

	if err := b.BankConfig.Validate(); err != nil {
		return err
	}

	return nil
}

func (b *Bank) GetLiabilityAmount(shares decimal.Decimal) (decimal.Decimal, error) {
	return shares.Mul(b.LiabilityShareValue), nil
}

func (b *Bank) GetAssetAmount(shares decimal.Decimal) (decimal.Decimal, error) {
	return shares.Mul(b.AssetShareValue), nil
}

func (b *Bank) GetAssetShares(value decimal.Decimal) (decimal.Decimal, error) {
	return value.Div(b.AssetShareValue), nil
}

func (b *Bank) GetLiabilityShares(value decimal.Decimal) (decimal.Decimal, error) {
	return value.Div(b.LiabilityShareValue), nil
}

func (b *Bank) ChangeAssetShares(shares decimal.Decimal, bypassDepositLimit bool) error {
	totalAssetShares := b.TotalAssetShares.Add(shares)
	b.TotalAssetShares = totalAssetShares

	if shares.IsPositive() && b.BankConfig.IsDepositLimitActive() && !bypassDepositLimit {
		totalDepositsAmount, err := b.GetAssetAmount(totalAssetShares)
		if err != nil {
			return err
		}
		depositLimit := b.BankConfig.DepositLimit

		if totalDepositsAmount.GreaterThan(depositLimit) {
			return BankAssetCapacityExceeded
		}
	}

	return nil
}

func (b *Bank) MaybeGetAssetWeightInitDiscount(price decimal.Decimal) (decimal.Decimal, error) {
	if b.BankConfig.UsdInitLimitActive() {
		bankTotalAssetsAmount, err := b.GetAssetAmount(b.TotalAssetShares)
		if err != nil {
			return decimal.Zero, err
		}
		bankTotalAssetsValue, err := CalcValue(bankTotalAssetsAmount, price, nil)
		if err != nil {
			return decimal.Zero, err
		}

		if bankTotalAssetsValue.IsZero() {
			return bankTotalAssetsValue, nil
		}

		totalAssetValueInitLimit := b.BankConfig.TotalAssetValueInitLimit
		if bankTotalAssetsValue.GreaterThanOrEqual(totalAssetValueInitLimit) {
			discount := totalAssetValueInitLimit.Div(bankTotalAssetsValue)
			return discount, nil
		}
		return decimal.Zero, nil
	}
	return decimal.Zero, nil
}

func (b *Bank) ChangeLiabilityShares(shares decimal.Decimal, bypassBorrowLimit bool) error {
	totalLiabilityShares := b.TotalLiabilityShares
	b.TotalLiabilityShares = totalLiabilityShares.Add(shares)

	if !bypassBorrowLimit && shares.IsPositive() && b.BankConfig.IsBorrowLimitActive() {
		totalLiabilityAmount, err := b.GetLiabilityAmount(b.TotalLiabilityShares)
		if err != nil {
			return err
		}
		borrowLimit := b.BankConfig.LiabilityLimit

		if totalLiabilityAmount.GreaterThanOrEqual(borrowLimit) {
			return BankLiabilityCapacityExceeded
		}
	}

	return nil
}

func (b *Bank) CheckUtilizationRatio() error {
	totalAssets, err := b.GetAssetAmount(b.TotalAssetShares)
	if err != nil {
		return err
	}
	totalLiabilities, err := b.GetLiabilityAmount(b.TotalLiabilityShares)
	if err != nil {
		return err
	}
	if totalAssets.LessThan(totalLiabilities) {
		return IllegalUtilizationRatio
	}

	return nil
}

func (b *Bank) AccrueInterest(log Log, currentTimestamp int64) error {
	timeDelta := currentTimestamp - b.LastUpdate

	if timeDelta <= 0 {
		return nil
	}
	b.LastUpdate = currentTimestamp

	totalAssets, err := b.GetAssetAmount(b.TotalAssetShares)
	if err != nil {
		return err
	}
	totalLiabilities, err := b.GetLiabilityAmount(b.TotalLiabilityShares)
	if err != nil {
		return err
	}
	if totalAssets.IsZero() || totalLiabilities.IsZero() {
		return nil
	}

	accruedAssetShareValue, accruedLiabilityShareValue, groupFeePaymentForPeriod, insuranceFeePaymentForPeriod, err :=
		CalcInterestRateAccrualStateChanges(log, uint64(timeDelta), totalAssets, totalLiabilities, b.BankConfig.InterestRateConfig, b.AssetShareValue, b.LiabilityShareValue)
	if err != nil {
		return err
	}

	b.AssetShareValue = accruedAssetShareValue
	b.LiabilityShareValue = accruedLiabilityShareValue
	b.CollectedGroupFeesOutstanding = b.CollectedGroupFeesOutstanding.Add(groupFeePaymentForPeriod)
	b.CollectedInsuranceFeesOutstanding = b.CollectedInsuranceFeesOutstanding.Add(insuranceFeePaymentForPeriod)

	// If the liquidity vault is positive, reduce the liquidity vault
	if b.LiquidityVault.IsPositive() {
		b.LiquidityVault = b.LiquidityVault.Sub(insuranceFeePaymentForPeriod).Sub(groupFeePaymentForPeriod)
		b.NormalizeLiquidityVault()
	}

	if b.LiquidityVault.IsNegative() {
		return ErrBankLiquidityDeficit
	}

	return nil
}

func (b *Bank) DepositSplTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
	*from = from.Sub(amount)
	*to = to.Add(amount)
}

func (b *Bank) WithdrawSplTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
	*from = from.Sub(amount)
	*to = to.Add(amount)
}

func (b *Bank) SocializeLoss(lossAmount decimal.Decimal) error {
	if b.TotalAssetShares.IsZero() || lossAmount.GreaterThanOrEqual(b.TotalAssetShares.Mul(b.AssetShareValue)) {
		return nil
	}

	totalAssetShares := b.TotalAssetShares
	oldAssetShareValue := b.AssetShareValue
	newShareValue := (totalAssetShares.Mul(oldAssetShareValue).Sub(lossAmount)).Div(totalAssetShares)
	b.AssetShareValue = newShareValue

	return nil
}

func (b *Bank) AssertOperationalMode(isAssetOrLiabilityAmountIncreasing bool) error {
	operationalState := b.BankConfig.OperationalState

	switch operationalState {
	case BankOperationalStatePaused:
		return BankPaused
	case BankOperationalStateOperational:
		return nil
	case BankOperationalStateReduceOnly:
		if isAssetOrLiabilityAmountIncreasing {
			return BankReduceOnly
		}
		return nil
	case BankOperationalStateNone:
		return nil
	}

	return nil
}

func (b *Bank) TransferFromInsuranceToLiquidity(amount decimal.Decimal) error {
	b.InsuranceVault = b.InsuranceVault.Sub(amount)
	b.LiquidityVault = b.LiquidityVault.Add(amount)
	return nil
}

func (b *Bank) DepositTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
	*from = from.Sub(amount)
	*to = to.Add(amount)
}

func (b *Bank) WithdrawTransfer(amount decimal.Decimal, from, to *decimal.Decimal) {
	*from = from.Sub(amount)
	*to = to.Add(amount)
}

func (b *Bank) GetTotalAssetQuantity() decimal.Decimal {
	return b.TotalAssetShares.Mul(b.AssetShareValue)
}

func (b *Bank) GetTotalLiabilityQuantity() decimal.Decimal {
	return b.TotalLiabilityShares.Mul(b.LiabilityShareValue)
}

func (b *Bank) GetAssetQuantity(assetShares decimal.Decimal) decimal.Decimal {
	return assetShares.Mul(b.AssetShareValue)
}

func (b *Bank) GetLiabilityQuantity(liabilityShares decimal.Decimal) decimal.Decimal {
	return liabilityShares.Mul(b.LiabilityShareValue)
}

func (b *Bank) ComputeAssetUsdValue(oraclePrice decimal.Decimal, assetShares decimal.Decimal, requirementType RequirementType, priceBias PriceBias) decimal.Decimal {
	assetQuantity := b.GetAssetQuantity(assetShares)
	assetWeight := b.GetAssetWeight(requirementType, oraclePrice, false)
	isWeighted := isWeightedPrice(requirementType)
	return b.ComputeUsdValue(oraclePrice, assetQuantity, priceBias, isWeighted, assetWeight, true)
}

func (b *Bank) ComputeLiabilityUsdValue(oraclePrice decimal.Decimal, liabilityShares decimal.Decimal, requirementType RequirementType, priceBias PriceBias) decimal.Decimal {
	liabilityQuantity := b.GetLiabilityQuantity(liabilityShares)
	liabilityWeight := b.GetLiabilityWeight(requirementType)
	isWeighted := isWeightedPrice(requirementType)
	return b.ComputeUsdValue(oraclePrice, liabilityQuantity, priceBias, isWeighted, liabilityWeight, true)
}

func (b *Bank) ComputeUsdValue(oraclePrice decimal.Decimal, quantity decimal.Decimal, priceBias PriceBias, weightedPrice bool, weight decimal.Decimal, scaleToBase bool) decimal.Decimal {
	price := b.GetPrice(oraclePrice, priceBias, weightedPrice)
	return quantity.Mul(price).Mul(weight)
}

func (b *Bank) GetPrice(oraclePrice decimal.Decimal, priceBias PriceBias, weightedPrice bool) decimal.Decimal {
	price := b.GetPriceWithConfidence(oraclePrice, weightedPrice)
	confidenceInterval := GetConfidenceInterval(price)
	switch priceBias {
	case Low:
		return price.Sub(confidenceInterval)
	case High:
		return price.Add(confidenceInterval)
	case Original:
		return price
	}
	return price
}

func (b *Bank) GetAssetWeight(requirementType RequirementType, oraclePrice decimal.Decimal, ignoreSoftLimits bool) decimal.Decimal {
	switch requirementType {
	case Initial:
		isSoftLimitDisabled := b.BankConfig.TotalAssetValueInitLimit.IsZero()
		if ignoreSoftLimits || isSoftLimitDisabled {
			return b.BankConfig.AssetWeightInit
		}
		totalBankCollateralValue := b.ComputeAssetUsdValue(oraclePrice, b.TotalAssetShares, Equity, Low)
		if totalBankCollateralValue.GreaterThan(b.BankConfig.TotalAssetValueInitLimit) {
			return b.BankConfig.TotalAssetValueInitLimit.Div(totalBankCollateralValue).Mul(b.BankConfig.AssetWeightInit)
		}
		return b.BankConfig.AssetWeightInit
	case Maintenance:
		return b.BankConfig.AssetWeightMaint
	case Equity:
		return ONE
	}
	return decimal.Zero
}

func (b *Bank) GetLiabilityWeight(requirementType RequirementType) decimal.Decimal {
	switch requirementType {
	case Initial:
		return b.BankConfig.LiabilityWeightInit
	case Maintenance:
		return b.BankConfig.LiabilityWeightMaint
	case Equity:
		return ONE
	}
	return decimal.Zero
}

func (b *Bank) ComputeTvl(oraclePrice decimal.Decimal) decimal.Decimal {
	return b.ComputeAssetUsdValue(oraclePrice, b.TotalAssetShares, Equity, Original).Sub(b.ComputeLiabilityUsdValue(oraclePrice, b.TotalLiabilityShares, Equity, Original))
}

func (b *Bank) GetPriceWithConfidence(oraclePrice decimal.Decimal, weighted bool) decimal.Decimal {
	return oraclePrice
}

func (b *Bank) NormalizeLiquidityVault() {
	if b.LiquidityVault.LessThan(EMPTY_BALANCE_THRESHOLD) {
		b.LiquidityVault = decimal.Zero
	}
}

func isWeightedPrice(requirementType RequirementType) bool {
	return requirementType == Initial
}

func (b *Bank) ComputeUtilizationRate() decimal.Decimal {
	totalDeposits := b.GetTotalAssetQuantity()
	if totalDeposits.IsZero() {
		return decimal.Zero
	}
	return b.GetTotalLiabilityQuantity().Div(totalDeposits)
}

func (b *Bank) ComputeRemainingCapacity(clk clock.Clock) (depositCapacity decimal.Decimal, borrowCapacity decimal.Decimal) {
	totalDeposits := b.GetTotalAssetQuantity()
	remainingCapacity := decimal.Max(decimal.Zero, b.BankConfig.DepositLimit.Sub(totalDeposits))

	totalBorrows := b.GetTotalLiabilityQuantity()
	remainingBorrowCapacity := decimal.Max(decimal.Zero, b.BankConfig.LiabilityLimit.Sub(totalBorrows))

	durationSinceLastAccrual := clk.Now().Unix() - b.LastUpdate

	lendingRate, borrowingRate, _, _, err := b.BankConfig.InterestRateConfig.CalcInterestRate(b.ComputeUtilizationRate())
	if err != nil {
		return decimal.Zero, decimal.Zero
	}

	outstandingLendingInterest := lendingRate.Mul(decimal.NewFromInt(durationSinceLastAccrual)).Div(decimal.NewFromInt(SECONDS_PER_YEAR)).Mul(totalDeposits)
	outstandingBorrowInterest := borrowingRate.Mul(decimal.NewFromInt(durationSinceLastAccrual)).Div(decimal.NewFromInt(SECONDS_PER_YEAR)).Mul(totalBorrows)

	depositCapacity = remainingCapacity.Sub(outstandingLendingInterest)
	borrowCapacity = remainingBorrowCapacity.Sub(outstandingBorrowInterest)

	return
}

func GetConfidenceInterval(price decimal.Decimal) decimal.Decimal {
	return price.Mul(MAX_CONF_INTERVAL)
}

type Emissions uint8

const (
	EmissionsInactive Emissions = iota
	EmissionsLending
	EmissionsBorrowing
)
