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
	// ReferralCode เป็น pointer เพราะจะถูกสร้างแบบ lazy (ครั้งแรกที่ผู้ใช้เปิดหน้าชวนเพื่อน) ก่อนหน้านั้นเป็น NULL
	ReferralCode *string `json:"referral_code"`
}

// ReferralInfo คือข้อมูลสรุปสำหรับหน้า "ชวนเพื่อน" ของผู้ใช้คนหนึ่ง
type ReferralInfo struct {
	ReferralCode string           `json:"referral_code"`
	TotalCoins   int              `json:"total_coins_earned"`
	TotalInvited int              `json:"total_invited"`
	Referrals    []ReferralRecord `json:"referrals"`
}

// ReferralRecord คือประวัติการชวนเพื่อน 1 รายการ
type ReferralRecord struct {
	ReferredUsername string    `json:"referred_username"`
	RewardCoins      int       `json:"reward_coins"`
	CreatedAt        time.Time `json:"created_at"`
}
