package accounts

import (
	"encoding/json"
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
)

type GameControlReset struct {
	At           time.Time `json:"reset_at"`
	AfterOrderID string    `json:"after_order_id"`
}

func gameControlReset(tx *bolt.Tx) (GameControlReset, error) {
	value, err := get[GameControlReset](tx, "settings", "game_control_reset")
	if errors.Is(err, ErrNotFound) {
		return GameControlReset{}, nil
	}
	return value, err
}

// Only advance the dashboard's reporting boundary. Orders, money and settlement
// remain intact, including pending orders created before this reset.
func (s *Store) ResetGameControl(actor, key string, now time.Time) (GameControlReset, error) {
	var out GameControlReset
	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := admin(tx, actor); err != nil {
			return err
		}
		body := struct{}{}
		prior, err := idem(tx, "game_control_reset:"+actor, key, body)
		if err != nil {
			return err
		}
		if prior != "" {
			return json.Unmarshal([]byte(prior), &out)
		}
		if now.IsZero() {
			now = time.Now()
		}
		last, _ := tx.Bucket([]byte("orders")).Cursor().Last()
		out = GameControlReset{At: now, AfterOrderID: string(last)}
		previous, err := gameControlReset(tx)
		if err != nil {
			return err
		}
		if err = put(tx, "settings", "game_control_reset", out); err != nil {
			return err
		}
		detail, err := json.Marshal(map[string]any{"before": previous, "after": out, "scope": "four_summary_cards"})
		if err != nil {
			return err
		}
		if err = audit(tx, actor, "game_control.reset", "summary_cards", string(detail)); err != nil {
			return err
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return remember(tx, "game_control_reset:"+actor, key, string(encoded), body)
	})
	return out, err
}
