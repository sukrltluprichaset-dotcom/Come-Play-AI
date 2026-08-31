package models

import "time"

type Payment struct {
	PaymentID   int64     `json:"payment_id"`
	UserID      int64     `json:"user_id"`
	Method      string    `json:"method"`
	Amount      float64   `json:"amount"`
	PackageName string    `json:"package_name"`
	CoinAmount  int       `json:"coin_amount"`
	CreatedAt   time.Time `json:"created_at"`
}
