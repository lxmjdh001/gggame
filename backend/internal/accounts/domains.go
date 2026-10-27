package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	bolt "go.etcd.io/bbolt"
	"io"
	"net/http"
	"starlight/backend/internal/domains"
)

func (h *HTTP) domainRequest(ctx context.Context, method, path string, input any) (json.RawMessage, error) {
	if h.DomainSocket == "" {
		return nil, errors.New("域名服务尚未安装，请先在服务器安装备用域名服务")
	}
	var body bytes.Buffer
	if input != nil {
		if e := json.NewEncoder(&body).Encode(input); e != nil {
			return nil, e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://local"+path, &body)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	client := domains.Client(h.DomainSocket)
	defer client.CloseIdleConnections()
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("域名服务暂不可用，请检查服务器域名服务状态")
	}
	defer res.Body.Close()
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if e = json.NewDecoder(io.LimitReader(res.Body, 131072)).Decode(&envelope); e != nil {
		return nil, errors.New("域名服务响应异常")
	}
	if res.StatusCode != 200 {
		return nil, errors.New(envelope.Error.Message)
	}
	if len(envelope.Data) == 0 {
		return nil, errors.New("域名服务响应为空")
	}
	return envelope.Data, nil
}

func (s *Store) recordDomainRequest(actor, action string, in any) error {
	raw, e := json.Marshal(in)
	if e != nil {
		return e
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if e := admin(tx, actor); e != nil {
			return e
		}
		return audit(tx, actor, "domains."+action, "backup-websites", string(raw))
	})
}
