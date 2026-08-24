package models

import "time"

type Review struct {
	ReviewID    int64     `json:"review_id"`
	CharacterID int64     `json:"character_id"`
	UserID      int64     `json:"user_id"`
	Username    string    `json:"username"`
	Rating      int       `json:"rating"`
	Comment     *string   `json:"comment"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
