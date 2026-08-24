package models

type Activity struct {
	ActivityID   int64   `json:"activity_id"`
	ActivityName string  `json:"activity_name"`
	Description  *string `json:"description"`
	RewardCoin   int     `json:"reward_coin"`
	IsRepeatable bool    `json:"is_repeatable"`
}
