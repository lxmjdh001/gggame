// Package client provides a typed, read-only client for the independent number service.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"starlight/gameapi/pkg/model"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("game API %d %s: %s", e.Status, e.Code, e.Message)
}
func New(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 5 * time.Second}}
}
func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := io.LimitReader(resp.Body, 2<<20)
	if resp.StatusCode != http.StatusOK {
		var e model.ErrorEnvelope
		_ = json.NewDecoder(body).Decode(&e)
		return &APIError{Status: resp.StatusCode, Code: e.Error.Code, Message: e.Error.Message}
	}
	return json.NewDecoder(body).Decode(dest)
}
func (c *Client) Latest(ctx context.Context) (model.Draw, error) { return c.Draw(ctx, "latest") }
func (c *Client) Draw(ctx context.Context, issue string) (model.Draw, error) {
	var data model.Envelope[model.Draw]
	err := c.get(ctx, "/api/v1/draws/"+url.PathEscape(issue), &data)
	return data.Data, err
}
func (c *Client) Select(ctx context.Context, issue, group string, positions []int) (model.Selection, error) {
	var data model.Envelope[model.Selection]
	path := "/api/v1/draws/" + url.PathEscape(issue) + "/groups/" + url.PathEscape(group)
	if len(positions) > 0 {
		parts := make([]string, len(positions))
		for i, p := range positions {
			parts[i] = strconv.Itoa(p)
		}
		path += "?" + url.Values{"positions": {strings.Join(parts, ",")}}.Encode()
	}
	err := c.get(ctx, path, &data)
	return data.Data, err
}
func (c *Client) History(ctx context.Context, limit int, before string) (model.Page, error) {
	var data model.Envelope[model.Page]
	q := url.Values{"limit": {strconv.Itoa(limit)}}
	if before != "" {
		q.Set("before", before)
	}
	err := c.get(ctx, "/api/v1/draws?"+q.Encode(), &data)
	return data.Data, err
}
