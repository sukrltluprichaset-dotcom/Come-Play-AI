package models

import "time"

type Report struct {
	ReportID    int64     `json:"report_id"`
	Details     string    `json:"details"`
	Status      string    `json:"status"`
	UserID      int64     `json:"user_id"`
	CharacterID int64     `json:"character_id"`
	CreatedAt   time.Time `json:"created_at"`
}
