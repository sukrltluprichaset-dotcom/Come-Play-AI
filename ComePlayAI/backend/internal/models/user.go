package models

import "time"

type User struct {
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	// AvatarURL เป็น pointer เพราะผู้ใช้ที่ยังไม่เคยตั้งรูปโปรไฟล์จะมีค่าเป็น NULL ในฐานข้อมูล
	AvatarURL *string `json:"avatar_url"`
}
