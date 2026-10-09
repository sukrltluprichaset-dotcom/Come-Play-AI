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

	// PopularityScore คือคะแนนยอดนิยมรวม (สเกล 0-5) ที่ผสมดาวรีวิวกับยอดคุยแล้ว ใช้แสดงเป็นรูปดาวในหน้าอันดับ
	// (มีค่าเฉพาะจาก endpoint /api/characters/popular endpoint อื่นจะเป็น 0)
	PopularityScore float64 `json:"popularity_score"`
}
