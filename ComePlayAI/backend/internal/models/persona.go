package models

import "time"

// Persona คือ "บทบาท/สถานการณ์ของผู้ใช้" ที่ผู้ใช้ตั้งค่าไว้เอง เพื่อให้ AI รับรู้และคุยด้วยตามบทบาทนั้น
// ผู้ใช้ 1 คนมีได้หลายบทบาท แต่เลือกใช้งานจริง (IsActive) ได้ทีละ 1 บทบาทเท่านั้น
type Persona struct {
	PersonaID   int64     `json:"persona_id"`
	UserID      int64     `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}
