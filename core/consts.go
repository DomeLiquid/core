package core

import (
	"github.com/shopspring/decimal"
)

const (
	MAX_UTXO_NUM         = 255
	AGGREGRATE_UTXO_MEMO = "arrgegate utxos"
)

const (
	SECONDS_PER_YEAR         = 31_536_000
	MIN_EMISSIONS_START_TIME = 1681989983

	HOURS_PER_YEAR = 365.25 * 24
)

var (
	ONE = decimal.NewFromInt(1)

	ZERO_AMOUNT_THRESHOLD   = decimal.Zero
	EMPTY_BALANCE_THRESHOLD = decimal.NewFromFloat(0.00000001)
	BANKRUPT_THRESHOLD      = decimal.NewFromFloat(0.00000001)
	MAX_CONF_INTERVAL       = decimal.NewFromFloat(0.05)

	LIQUIDATION_LIQUIDATOR_FEE = decimal.NewFromFloat(0.0025)
	LIQUIDATION_INSURANCE_FEE  = decimal.NewFromFloat(0.0025)
)
