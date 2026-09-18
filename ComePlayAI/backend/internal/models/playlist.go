package models

import "time"

// UserPlaylist คือเพลย์ลิสต์ที่ผู้ใช้สร้างขึ้นเองเพื่อจัดกลุ่มตัวละครที่ชอบไว้เป็นหมวดหมู่
// (เช่น "คู่จิ้น", "เพื่อนคุยเล่น") แล้วแสดงเป็นแถวๆ ในหน้าแรกของผู้ใช้คนนั้นโดยเฉพาะ เรียงตาม DisplayOrder
type UserPlaylist struct {
	PlaylistID   int64       `json:"playlist_id"`
	UserID       int64       `json:"user_id"`
	Name         string      `json:"name"`
	DisplayOrder int         `json:"display_order"`
	CreatedAt    time.Time   `json:"created_at"`
	Characters   []Character `json:"characters"`
}
