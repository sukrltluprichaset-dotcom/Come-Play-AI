package models

import "time"

type Diary struct {
	DiaryID       int64     `json:"diary_id"`
	UserID        int64     `json:"user_id"`
	CharacterID   int64     `json:"character_id"`
	CharacterName string    `json:"character_name"`
	EntryDate     time.Time `json:"entry_date"`
	Summary       string    `json:"summary"`
	CreatedAt     time.Time `json:"created_at"`
}
