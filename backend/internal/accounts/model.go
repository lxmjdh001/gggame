package accounts

import (
	"starlight/backend/internal/markets"
	"time"
)

type User struct {
	SecondaryPasswordSet     bool      `json:"secondary_password_set"`
	SecondaryPasswordEnabled bool      `json:"secondary_password_enabled"`
	Wechat                   string    `json:"wechat,omitempty"`
	WithdrawalPasswordSet    bool      `json:"withdrawal_password_set"`
	SecurityQuestionSet      bool      `json:"security_question_set"`
	Phone                    string    `json:"phone,omitempty"`
	Name                     string    `json:"name,omitempty"`
	InviterUsername          string    `json:"inviter_username,omitempty"`
	ID                       string    `json:"id"`
	Username                 string    `json:"username"`
	Role                     string    `json:"role"`
	Status                   string    `json:"status"`
	Deleted                  bool      `json:"deleted,omitempty"`
	InviteCode               string    `json:"invite_code"`
	InviterID                string    `json:"inviter_id,omitempty"`
	Balance                  int64     `json:"balance_cents"`
	Frozen                   int64     `json:"frozen_cents"`
	CreatedAt                time.Time `json:"created_at"`
}
type storedUser struct {
	User
	DeletionRevision   string `json:",omitempty"`
	SecondaryHash      string
	SecondarySalt      string
	SecondaryRevision  string
	Hash               string
	PasswordSalt       string
	WithdrawalHash     string
	WithdrawalSalt     string
	SecurityQuestionID string
	SecurityHash       string
	SecuritySalt       string
}
type Login struct {
	UserID              string
	Expires             time.Time
	SecondaryVerifiedAt time.Time
	SecondaryRevision   string
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
	Withdrawal      *WithdrawalTerms `json:"withdrawal,omitempty"`
	PaymentMethod   string           `json:"payment_method,omitempty"`
	USDTAmount      string           `json:"usdt_amount,omitempty"`
	TransactionID   string           `json:"transaction_id,omitempty"`
	SettingsVersion int64            `json:"settings_version,omitempty"`
	PaymentNetwork  string           `json:"payment_network,omitempty"`
	PaymentAddress  string           `json:"payment_address,omitempty"`
	RateYuan        string           `json:"rate_yuan,omitempty"`
	ID              string           `json:"id"`
	UserID          string           `json:"user_id"`
	Username        string           `json:"username"`
	Kind            string           `json:"kind"`
	Amount          int64            `json:"amount_cents"`
	Note            string           `json:"note"`
	Status          string           `json:"status"`
	ReviewNote      string           `json:"review_note"`
	CreatedAt       time.Time        `json:"created_at"`
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
	MarketID         string           `json:"market_id,omitempty"`
	MarketName       string           `json:"market_name,omitempty"`
	OddsMilli        int64            `json:"odds_milli,omitempty"`
	TierOddsMilli    map[string]int64 `json:"tier_odds_milli,omitempty"`
	PayoutTier       string           `json:"payout_tier,omitempty"`
	PayoutMultiplier int64            `json:"payout_multiplier,omitempty"`
	PayoutOddsMilli  int64            `json:"payout_odds_milli,omitempty"`
	RuleSnapshot     *markets.Market  `json:"rule_snapshot,omitempty"`
	Outcome          string           `json:"outcome,omitempty"`
	ResultNumbers    []int            `json:"result_numbers,omitempty"`
	SettledAt        *time.Time       `json:"settled_at,omitempty"`
	ID               string           `json:"id"`
	UserID           string           `json:"user_id"`
	Username         string           `json:"username"`
	SessionID        string           `json:"session_id"`
	SessionName      string           `json:"session_name"`
	GameName         string           `json:"game_name"`
	Issue            string           `json:"issue"`
	Numbers          []int            `json:"numbers"`
	Multiplier       int              `json:"multiplier"`
	Cost             int64            `json:"cost_cents"`
	Payout           int64            `json:"payout_cents"`
	Status           string           `json:"status"`
	InviterID        string           `json:"inviter_id,omitempty"`
	CommissionBPS    [3]int64         `json:"commission_bps"`
	Ancestors        []string         `json:"ancestors"`
	Note             string           `json:"note"`
	CreatedAt        time.Time        `json:"created_at"`
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
	gameToken   string
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
