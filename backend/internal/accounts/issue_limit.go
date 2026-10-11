package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	bolt "go.etcd.io/bbolt"
)

const defaultUserIssueLimit int64 = 2000000 // 20,000 yuan, stored in cents.
const issueStakeBucket = "user_issue_stakes"

func issueStakeKey(user, session, issue string) string { return user + "|" + session + "|" + issue }
func issueStake(tx *bolt.Tx, user, session, issue string) (int64, error) {
	data := tx.Bucket([]byte(issueStakeBucket)).Get([]byte(issueStakeKey(user, session, issue)))
	if data == nil {
		return 0, nil
	}
	var used int64
	e := json.Unmarshal(data, &used)
	return used, e
}

// Migrate existing accepted orders exactly once. Refunded orders do not occupy capacity.
func initializeIssueStakes(tx *bolt.Tx) error {
	if tx.Bucket([]byte(issueStakeBucket)) != nil {
		return nil
	}
	if _, e := tx.CreateBucket([]byte(issueStakeBucket)); e != nil {
		return e
	}
	return tx.Bucket([]byte("orders")).ForEach(func(_, v []byte) error {
		var o Order
		if e := json.Unmarshal(v, &o); e != nil {
			return e
		}
		if o.Status != "pending" && o.Status != "settled" {
			return nil
		}
		used, e := issueStake(tx, o.UserID, o.SessionID, o.Issue)
		if e != nil {
			return e
		}
		return put(tx, issueStakeBucket, issueStakeKey(o.UserID, o.SessionID, o.Issue), used+o.Cost)
	})
}

// Called in the same write transaction as account debit/order insertion, after idempotency lookup.
func reserveIssueStake(tx *bolt.Tx, user, session, issue string, cost, limit int64) error {
	if limit <= 0 || cost <= 0 {
		return errors.New("单用户单期投注限额配置异常")
	}
	used, e := issueStake(tx, user, session, issue)
	if e != nil {
		return e
	}
	remaining := max(int64(0), limit-used)
	if cost > remaining {
		return fmt.Errorf("已超出单用户单期投注上限，本期剩余额度 %d.%02d 元", remaining/100, remaining%100)
	}
	return put(tx, issueStakeBucket, issueStakeKey(user, session, issue), used+cost)
}
func releaseIssueStake(tx *bolt.Tx, o Order) error {
	used, e := issueStake(tx, o.UserID, o.SessionID, o.Issue)
	if e != nil {
		return e
	}
	if used < o.Cost {
		return errors.New("本期累计投注金额异常，请核对订单")
	}
	return put(tx, issueStakeBucket, issueStakeKey(o.UserID, o.SessionID, o.Issue), used-o.Cost)
}
