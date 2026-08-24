package models

import "time"

type Evaluation struct {
	EvalID    int64     `json:"eval_id"`
	Question  string    `json:"question"`
	Answer    int       `json:"answer"`
	UserID    int64     `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}
