package models

import "time"

type Character struct {
	CharacterID int64     `json:"character_id"`
	Name        string    `json:"name"`
	Personality string    `json:"personality"`
	AvatarURL   *string   `json:"avatar_url"`
	UsageCount  int       `json:"usage_count"`
	Rating      float64   `json:"rating"`
	ReviewCount int       `json:"review_count"`
	IsShared    bool      `json:"is_shared"`
	UserID      int64     `json:"user_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
