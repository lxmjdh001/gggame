package accounts

import (
	"bytes"
	"encoding/json"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestLegacyCopyPresentationDoesNotRewriteFinancialRecords(t *testing.T) {
	s, admin := setup(t)
	member := register(t, s, "copy_member", "")
	legacy := Ledger{ID: "old-bonus", UserID: member.ID, Kind: "registration_bonus", Note: "注册赠送（开发测试）", Amount: 200, Balance: 200}
	custom := Ledger{ID: "custom-note", UserID: member.ID, Kind: "admin_adjust", Note: "测试备注由管理员填写", Amount: 100, Balance: 300}
	must(t, s.db.Update(func(tx *bolt.Tx) error {
		if err := put(tx, "ledger", legacy.ID, legacy); err != nil {
			return err
		}
		if err := put(tx, "ledger", custom.ID, custom); err != nil {
			return err
		}
		cfg, err := get[Settings](tx, "settings", "global")
		if err != nil {
			return err
		}
		cfg.Announcement = "欢迎来到星彩娱乐。账户与开奖记录已同步至服务器。"
		return put(tx, "settings", "global", cfg)
	}))
	var beforeSettings []byte
	must(t, s.db.View(func(tx *bolt.Tx) error {
		beforeSettings = bytes.Clone(tx.Bucket([]byte("settings")).Get([]byte("global")))
		return nil
	}))
	account, err := s.Account(member.ID)
	must(t, err)
	for _, row := range account["ledger"].([]Ledger) {
		if row.ID == legacy.ID && (row.Note != "注册赠送" || row.Amount != legacy.Amount || row.Balance != legacy.Balance) {
			t.Fatal("incorrect legacy display")
		}
		if row.ID == custom.ID && row.Note != custom.Note {
			t.Fatal("custom note changed")
		}
	}
	adminRows, err := s.AdminData(admin.ID, "ledger", "", 1)
	must(t, err)
	raw, err := json.Marshal(adminRows)
	must(t, err)
	if bytes.Contains(raw, []byte(legacy.Note)) || !bytes.Contains(raw, []byte(custom.Note)) {
		t.Fatal("admin display mismatch")
	}
	cfg, err := s.Settings()
	must(t, err)
	if cfg.Announcement != defaults().Announcement {
		t.Fatal("legacy default copy not updated")
	}
	must(t, s.db.View(func(tx *bolt.Tx) error {
		for _, row := range []Ledger{legacy, custom} {
			want, _ := json.Marshal(row)
			if !bytes.Equal(tx.Bucket([]byte("ledger")).Get([]byte(row.ID)), want) {
				t.Fatal("read rewrote financial record")
			}
		}
		if !bytes.Equal(beforeSettings, tx.Bucket([]byte("settings")).Get([]byte("global"))) {
			t.Fatal("read rewrote stored settings")
		}
		return nil
	}))
	cfg.Announcement = "自定义站点公告"
	must(t, s.SaveSettings(admin.ID, cfg))
	after, err := s.Settings()
	must(t, err)
	if after.Announcement != cfg.Announcement {
		t.Fatal("custom announcement overridden")
	}
}
