package accounts

import (
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type PromotionInput struct {
	Title   string `json:"title"`
	Tagline string `json:"tagline"`
	Summary string `json:"summary"`
	Content string `json:"content"`
	Button  string `json:"button"`
	Action  string `json:"action"`
	Theme   string `json:"theme"`
	Icon    string `json:"icon"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort"`
	Version int    `json:"version"`
}
type Promotion struct {
	PromotionInput
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

var promotionThemes = []string{"lavender", "mint", "peach", "sky", "rose", "gold"}

func seedPromotions(tx *bolt.Tx) error {
	if tx.Bucket([]byte("promotions")) != nil {
		return nil
	}
	if _, e := tx.CreateBucket([]byte("promotions")); e != nil {
		return e
	}
	for _, p := range []Promotion{
		{PromotionInput: PromotionInput{Title: "邀请好友，三级收益", Tagline: "一起发现精彩", Summary: "分享专属邀请链接，查看团队成长。\n有效投注佣金，清晰记录每一笔。", Content: "分享个人邀请链接，好友注册后即可建立邀请关系。团队已结算的有效投注按当前佣金规则计算收益，可在邀请与佣金页面查看明细。", Button: "查看邀请", Action: "commissions", Theme: "lavender", Icon: "gift", Enabled: true, Sort: 10, Version: 1}, ID: "invitation", UpdatedAt: time.Now()},
		{PromotionInput: PromotionInput{Title: "喜欢的，随时都在", Tagline: "收藏你的热爱", Summary: "将喜爱的游戏加入收藏，\n下一次见面，一键直达。", Content: "点击游戏卡片上的爱心即可收藏。在游戏大厅选择“我的收藏”，快速找到喜爱的游戏。", Button: "查看我的收藏", Action: "favorites", Theme: "mint", Icon: "heart", Enabled: true, Sort: 20, Version: 1}, ID: "favorites", UpdatedAt: time.Now()},
	} {
		if e := put(tx, "promotions", p.ID, p); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Promotions(publishedOnly bool) ([]Promotion, error) {
	items := []Promotion{}
	e := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("promotions")).ForEach(func(_, v []byte) error {
			var p Promotion
			if e := json.Unmarshal(v, &p); e != nil {
				return e
			}
			if !publishedOnly || p.Enabled {
				items = append(items, p)
			}
			return nil
		})
	})
	sort.Slice(items, func(i, j int) bool {
		if items[i].Sort != items[j].Sort {
			return items[i].Sort < items[j].Sort
		}
		return items[i].ID < items[j].ID
	})
	return items, e
}
func (s *Store) SavePromotion(actor, id string, in PromotionInput) (Promotion, error) {
	var out Promotion
	if len(id) < 3 || len(id) > 64 || strings.ContainsAny(id, "/\\ ") {
		return out, errors.New("活动编号无效")
	}
	in.Title = strings.TrimSpace(in.Title)
	in.Tagline = strings.TrimSpace(in.Tagline)
	in.Summary = strings.TrimSpace(in.Summary)
	in.Content = strings.TrimSpace(in.Content)
	in.Button = strings.TrimSpace(in.Button)
	if in.Title == "" || in.Summary == "" || in.Button == "" || utf8.RuneCountInString(in.Title) > 40 || utf8.RuneCountInString(in.Tagline) > 30 || utf8.RuneCountInString(in.Summary) > 200 || utf8.RuneCountInString(in.Content) > 10000 || utf8.RuneCountInString(in.Button) > 20 || in.Sort < 0 || in.Sort > 9999 || in.Version < 0 {
		return out, errors.New("请检查活动内容长度和排序值")
	}
	if !oneOf(in.Action, []string{"details", "commissions", "favorites", "home", "wallet"}) || !oneOf(in.Icon, []string{"gift", "heart", "sparkles", "ticket", "trophy", "party"}) || in.Theme != "" && !oneOf(in.Theme, promotionThemes) {
		return out, errors.New("活动按钮、图标或配色无效")
	}
	e := s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		var old *Promotion
		if data := tx.Bucket([]byte("promotions")).Get([]byte(id)); data != nil {
			var p Promotion
			if e := json.Unmarshal(data, &p); e != nil {
				return e
			}
			old = &p
			if in.Version != p.Version {
				return errors.New("活动已被更新，请刷新后重新编辑")
			}
		} else if in.Version != 0 {
			return ErrNotFound
		}
		if in.Theme == "" {
			if old != nil {
				in.Theme = old.Theme
			} else {
				counts := map[string]int{}
				if e := tx.Bucket([]byte("promotions")).ForEach(func(_, v []byte) error {
					var p Promotion
					if e := json.Unmarshal(v, &p); e != nil {
						return e
					}
					counts[p.Theme]++
					return nil
				}); e != nil {
					return e
				}
				in.Theme = promotionThemes[0]
				for _, theme := range promotionThemes {
					if counts[theme] < counts[in.Theme] {
						in.Theme = theme
					}
				}
			}
		}
		in.Version++
		out = Promotion{PromotionInput: in, ID: id, UpdatedAt: time.Now()}
		if e := put(tx, "promotions", id, out); e != nil {
			return e
		}
		detail, _ := json.Marshal(map[string]any{"before": old, "after": out})
		return audit(tx, actor, "promotion.save", id, string(detail))
	})
	return out, e
}
func oneOf(v string, values []string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}
func (s *Store) DeletePromotion(actor, id string, version int) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		p, e := get[Promotion](tx, "promotions", id)
		if e != nil {
			return e
		}
		if p.Version != version {
			return errors.New("活动已被更新，请刷新后重试")
		}
		if e = tx.Bucket([]byte("promotions")).Delete([]byte(id)); e != nil {
			return e
		}
		detail, _ := json.Marshal(p)
		return audit(tx, actor, "promotion.delete", id, string(detail))
	})
}
