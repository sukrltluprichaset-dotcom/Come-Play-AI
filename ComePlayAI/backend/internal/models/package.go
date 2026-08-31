package models

type Package struct {
	PackageID  int64   `json:"package_id"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	CoinAmount int     `json:"coin_amount"`
}
