package models

type ReportAdminView struct {
	ReportID      int64  `json:"report_id"`
	Details       string `json:"details"`
	Status        string `json:"status"`
	CharacterID   int64  `json:"character_id"`
	CharacterName string `json:"character_name"`
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`
	CreatedAt     string `json:"created_at"`
}
