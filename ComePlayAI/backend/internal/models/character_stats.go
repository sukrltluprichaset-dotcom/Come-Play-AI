package models

type CharacterStats struct {
	CharacterID    int64   `json:"character_id"`
	Name           string  `json:"name"`
	AvatarURL      *string `json:"avatar_url"`
	UsageCount     int     `json:"usage_count"`
	UniqueChatters int     `json:"unique_chatters"`
	Rating         float64 `json:"rating"`
	ReviewCount    int     `json:"review_count"`
}
