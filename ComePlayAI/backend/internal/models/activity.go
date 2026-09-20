package models

type Activity struct {
	ActivityID   int64   `json:"activity_id"`
	ActivityName string  `json:"activity_name"`
	Description  *string `json:"description"`
	RewardCoin   int     `json:"reward_coin"`
	IsRepeatable bool    `json:"is_repeatable"`
	// ClaimedToday บอกว่าผู้ใช้คนนี้เข้าร่วม/กดรับกิจกรรมนี้ไปแล้วหรือยัง (ถ้า repeatable เช็คเฉพาะวันนี้
	// ถ้าไม่ repeatable เช็คว่าเคยรับไปแล้วครั้งใดก็ตาม) ให้ frontend เอาไปซ่อนปุ่ม/แบนเนอร์ที่กดซ้ำไม่ได้แล้ว
	// แทนที่จะปล่อยให้กดแล้วเจอ error "เคยรับไปแล้ว" ทุกครั้งที่กดซ้ำ
	ClaimedToday bool `json:"claimed_today"`
}
