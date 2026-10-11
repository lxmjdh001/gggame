package accounts

import (
	"starlight/backend/internal/markets"
	"time"
)

type User struct {
	InviterUsername string    `json:"inviter_username,omitempty"`
	ID              string    `json:"id"`
	Username        string    `json:"username"`
	Role            string    `json:"role"`
	Status          string    `json:"status"`
	InviteCode      string    `json:"invite_code"`
	InviterID       string    `json:"inviter_id,omitempty"`
	Balance         int64     `json:"balance_cents"`
	Frozen          int64     `json:"frozen_cents"`
	CreatedAt       time.Time `json:"created_at"`
}
type storedUser struct {
	User
	Hash         string
	PasswordSalt string
}
type Login struct {
	UserID  string
	Expires time.Time
}
type Ledger struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Kind      string    `json:"kind"`
	Amount    int64     `json:"amount_cents"`
	Balance   int64     `json:"balance_cents"`
	Frozen    int64     `json:"frozen_cents"`
	Reference string    `json:"reference"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}
type WalletRequest struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Username   string    `json:"username"`
	Kind       string    `json:"kind"`
	Amount     int64     `json:"amount_cents"`
	Note       string    `json:"note"`
	Status     string    `json:"status"`
	ReviewNote string    `json:"review_note"`
	CreatedAt  time.Time `json:"created_at"`
}
type PlaySetting struct {
	UserIssueLimitCents int64  `json:"user_issue_limit_cents"`
	SessionID           string `json:"session_id"`
	Enabled             bool   `json:"enabled"`
	UnitCents           int64  `json:"unit_cents"`
	MaxMultiplier       int    `json:"max_multiplier"`
	CloseSeconds        int    `json:"close_seconds"`
	MaxStakeCents       int64  `json:"max_stake_cents"`
}
type Settings struct {
	RegistrationBonusCents int64         `json:"registration_bonus_cents"`
	InviteRequired         bool          `json:"invite_required"`
	Registration           bool          `json:"registration"`
	CommissionBPS          [3]int64      `json:"commission_bps"`
	Announcement           string        `json:"announcement"`
	Plays                  []PlaySetting `json:"plays"`
}
type Order struct {
	MarketID      string          `json:"market_id,omitempty"`
	MarketName    string          `json:"market_name,omitempty"`
	OddsMilli     int64           `json:"odds_milli,omitempty"`
	RuleSnapshot  *markets.Market `json:"rule_snapshot,omitempty"`
	Outcome       string          `json:"outcome,omitempty"`
	ResultNumbers []int           `json:"result_numbers,omitempty"`
	SettledAt     *time.Time      `json:"settled_at,omitempty"`
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	Username      string          `json:"username"`
	SessionID     string          `json:"session_id"`
	SessionName   string          `json:"session_name"`
	GameName      string          `json:"game_name"`
	Issue         string          `json:"issue"`
	Numbers       []int           `json:"numbers"`
	Multiplier    int             `json:"multiplier"`
	Cost          int64           `json:"cost_cents"`
	Payout        int64           `json:"payout_cents"`
	Status        string          `json:"status"`
	InviterID     string          `json:"inviter_id,omitempty"`
	CommissionBPS [3]int64        `json:"commission_bps"`
	Ancestors     []string        `json:"ancestors"`
	Note          string          `json:"note"`
	CreatedAt     time.Time       `json:"created_at"`
}
type Commission struct {
	Level        int       `json:"level"`
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	FromUserID   string    `json:"from_user_id"`
	FromUsername string    `json:"from_username"`
	OrderID      string    `json:"order_id"`
	ValidStake   int64     `json:"valid_stake_cents"`
	Rate         int64     `json:"rate_bps"`
	Amount       int64     `json:"amount_cents"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
}
type Audit struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}
type OrderInput struct {
	AmountCents *int64 `json:"amount_cents,omitempty"`
	UnitCents   int64  `json:"unit_cents"`
	SessionID   string `json:"session_id"`
	Issue       string `json:"issue"`
	Numbers     []int  `json:"numbers"`
	Multiplier  int    `json:"multiplier"`
	Key         string `json:"key"`
}
type RequestInput struct {
	Kind   string `json:"kind"`
	Amount int64  `json:"amount_cents"`
	Note   string `json:"note"`
	Key    string `json:"key"`
}
