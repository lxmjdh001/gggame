package accounts

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

type HelpArticleInput struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Enabled bool   `json:"enabled"`
	Sort    int    `json:"sort"`
	Version int    `json:"version"`
}
type HelpArticle struct {
	HelpArticleInput
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

func seedHelpArticles(tx *bolt.Tx) error {
	// The bucket is the initialization marker; deleting all articles must stay empty.
	if tx.Bucket([]byte("help_articles")) != nil {
		return nil
	}
	if _, err := tx.CreateBucket([]byte("help_articles")); err != nil {
		return err
	}
	for i, item := range []struct{ id, title, content string }{
		{"betting", "如何提交投注？", "在大厅选择游戏和场次，可同时勾选多个投注选项，在快捷下注区填写每注金额后提交。六号单式请按页面要求选号。"},
		{"records", "如何查看账户与订单？", "登录后可在个人中心查看余额，在订单记录中查看投注与结算结果。游戏收藏保存在当前浏览器。"},
		{"wallet", "充值与提现如何办理？", "请在钱包页面提交充值或提现申请，由管理员核对处理。充值经核实后入账；提现申请提交后冻结对应余额，驳回后自动解冻。可在申请记录中查看处理进度。"},
		{"assistance", "如何获取人工帮助？", "如需人工处理账户或资金问题，请联系为您提供服务的管理员。"},
	} {
		p := HelpArticle{HelpArticleInput: HelpArticleInput{Title: item.title, Content: item.content, Enabled: true, Sort: (i + 1) * 10, Version: 1}, ID: item.id, UpdatedAt: time.Now()}
		if err := put(tx, "help_articles", p.ID, p); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) HelpArticles(publishedOnly bool) ([]HelpArticle, error) {
	items := []HelpArticle{}
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("help_articles")).ForEach(func(_, raw []byte) error {
			var p HelpArticle
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
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
	return items, err
}

var helpIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,64}$`)

func (s *Store) SaveHelpArticle(actor, id string, in HelpArticleInput) (HelpArticle, error) {
	var out HelpArticle
	in.Title, in.Content = strings.TrimSpace(in.Title), strings.TrimSpace(in.Content)
	if !helpIDPattern.MatchString(id) || in.Title == "" || in.Content == "" || utf8.RuneCountInString(in.Title) > 80 || utf8.RuneCountInString(in.Content) > 10000 || in.Sort < 0 || in.Sort > 9999 || in.Version < 0 {
		return out, errors.New("请检查帮助标题、内容和排序值")
	}
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		var old *HelpArticle
		if raw := tx.Bucket([]byte("help_articles")).Get([]byte(id)); raw != nil {
			var p HelpArticle
			if err := json.Unmarshal(raw, &p); err != nil {
				return err
			}
			old = &p
			if in.Version != p.Version {
				return errors.New("内容已被更新，请刷新列表后重新编辑")
			}
		} else if in.Version != 0 {
			return ErrNotFound
		}
		in.Version++
		out = HelpArticle{HelpArticleInput: in, ID: id, UpdatedAt: time.Now()}
		if err := put(tx, "help_articles", id, out); err != nil {
			return err
		}
		detail, _ := json.Marshal(map[string]any{"before": old, "after": out})
		return audit(tx, actor, "help.save", id, string(detail))
	})
	return out, err
}

func (s *Store) DeleteHelpArticle(actor, id string, version int) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		p, err := get[HelpArticle](tx, "help_articles", id)
		if err != nil {
			return err
		}
		if p.Version != version {
			return errors.New("内容已被更新，请刷新列表后重新确认删除")
		}
		if err := tx.Bucket([]byte("help_articles")).Delete([]byte(id)); err != nil {
			return err
		}
		detail, _ := json.Marshal(p)
		return audit(tx, actor, "help.delete", id, string(detail))
	})
}
