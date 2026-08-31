package models

import "time"

type Payment struct {
	PaymentID     int64     `json:"payment_id"`
	UserID        int64     `json:"user_id"`
	PackageID     int64     `json:"package_id"`
	PaymentMethod string    `json:"payment_method"`
	Amount        float64   `json:"amount"`
	Status        string    `json:"status"`
	PackageName   string    `json:"package_name"`
	CoinAmount    int       `json:"coin_amount"`
	PaymentTime   time.Time `json:"payment_time"`
}
