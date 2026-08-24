package models

import "time"

type Coin struct {
	CoinID    int64     `json:"coin_id"`
	Balance   int       `json:"balance"`
	UserID    int64     `json:"user_id"`
	UpdatedAt time.Time `json:"updated_at"`
}
