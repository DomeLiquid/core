package core

type OracleSetup uint8

func (os OracleSetup) String() string {
	switch os {
	case MixinOracle:
		return "Mixin"
	default:
		return "Unknown"
	}
}

const (
	MixinOracle OracleSetup = iota
)

type OraclePriceType uint8

const (
	TimeWeighted OraclePriceType = iota
	RealTime
)

type PriceBias uint8

const (
	Low PriceBias = iota
	High
	Original
)
