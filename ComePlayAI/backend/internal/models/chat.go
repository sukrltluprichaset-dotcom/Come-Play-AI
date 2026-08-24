package models

import "time"

type Chat struct {
	ChatID      int64     `json:"chat_id"`
	SenderType  string    `json:"sender_type"`
	Message     string    `json:"message"`
	SendTime    time.Time `json:"send_time"`
	UserID      int64     `json:"user_id"`
	CharacterID int64     `json:"character_id"`
}
