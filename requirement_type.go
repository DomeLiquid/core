package core

type RequirementType uint8

const (
	Initial RequirementType = iota
	Maintenance
	Equity
)

func (rt RequirementType) GetOraclePriceType() OraclePriceType {
	switch rt {
	case Initial, Equity:
		return TimeWeighted
	case Maintenance:
		return RealTime
	default:
		return TimeWeighted
	}
}
