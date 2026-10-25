package accounts

import (
	bolt "go.etcd.io/bbolt"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

func TestAgentTodayStakeAllDepthBeijingBoundariesAndMoves(t *testing.T) {
	s := agentFixture(t)
	start := time.Date(2026, 10, 25, 0, 0, 0, 0, model.Beijing)
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		for _, o := range []Order{{UserID: "a", Status: "settled", Cost: 100, CreatedAt: start}, {UserID: "b", Status: "pending", Cost: 200, CreatedAt: start.Add(time.Second)}, {UserID: "c", Status: "settled", Cost: 300, CreatedAt: start.Add(23 * time.Hour)}, {UserID: "d", Status: "settled", Cost: 400, CreatedAt: start.Add(24*time.Hour - time.Nanosecond)}, {UserID: "d", Status: "refunded", Cost: 9999, CreatedAt: start}, {UserID: "c", Status: "settled", Cost: 9999, CreatedAt: start.Add(-time.Nanosecond)}, {UserID: "c", Status: "pending", Cost: 9999, CreatedAt: start.Add(24 * time.Hour)}} {
			o.ID = seq(tx, "orders")
			if e := put(tx, "orders", o.ID, o); e != nil {
				return e
			}
		}
		return nil
	}))
	p, e := s.agentTreeAt("admin", "a", "", 1, start.Add(time.Hour).UTC())
	must(t, e)
	if p.Day != "2026-10-25" || p.TodayStake != 1000 || p.Items[0].TodayStake != 100 || p.Items[0].TeamTodayStake != 900 {
		t.Fatal(p)
	}
	state, e := s.PreviewMemberInviter("admin", "c", MemberInviterInput{InviterUsername: "sibling"})
	must(t, e)
	_, e = s.ChangeMemberInviter("admin", "c", MemberInviterInput{InviterUsername: "sibling", Revision: state.Revision, Note: "当日流水同步归属", Key: randomID()})
	must(t, e)
	p, e = s.agentTreeAt("admin", "a", "", 1, start)
	must(t, e)
	next, e := s.agentTreeAt("admin", "sibling", "", 1, start)
	must(t, e)
	if p.TodayStake != 300 || next.TodayStake != 700 {
		t.Fatal(p.TodayStake, next.TodayStake)
	}
	all, e := s.agentTreeAt("admin", "", "", 1, start)
	must(t, e)
	if all.TodayStake != 1000 {
		t.Fatal("double counted totals", all.TodayStake)
	}
}
