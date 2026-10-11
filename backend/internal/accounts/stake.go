package accounts

import "errors"

// Direct amounts use integer cents; legacy multiplier requests remain compatible.
func orderStake(p PlaySetting, amount *int64, multiplier int) (int64, error) {
	if amount != nil {
		if multiplier != 0 {
			return 0, errors.New("投注金额与倍数不能同时提交")
		}
		if *amount < p.UnitCents || *amount > p.MaxStakeCents {
			return 0, errors.New("投注金额超出该场次允许范围")
		}
		return *amount, nil
	}
	if multiplier < 1 || multiplier > p.MaxMultiplier {
		return 0, errors.New("倍数超出允许范围")
	}
	return p.UnitCents * int64(multiplier), nil
}
