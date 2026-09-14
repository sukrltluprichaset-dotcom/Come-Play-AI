package handlers

import (
	"database/sql"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type ActivityHandler struct {
	DB *sql.DB
}

func NewActivityHandler(db *sql.DB) *ActivityHandler {
	return &ActivityHandler{DB: db}
}

func (h *ActivityHandler) List(c *fiber.Ctx) error {
	rows, err := h.DB.Query(
		`SELECT activity_id, activity_name, description, reward_coin, is_repeatable
		 FROM activities WHERE is_active = true ORDER BY activity_id ASC`,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายการกิจกรรมไม่สำเร็จ")
	}
	defer rows.Close()

	activities := []models.Activity{}
	for rows.Next() {
		var a models.Activity
		var description sql.NullString
		if err := rows.Scan(&a.ActivityID, &a.ActivityName, &description, &a.RewardCoin, &a.IsRepeatable); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายการกิจกรรมไม่สำเร็จ")
		}
		if description.Valid {
			a.Description = &description.String
		}
		activities = append(activities, a)
	}

	return writeJSON(c, fiber.StatusOK, activities)
}

func (h *ActivityHandler) Claim(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	activityID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสกิจกรรมไม่ถูกต้อง")
	}

	var rewardCoin int
	var isRepeatable, isActive bool
	err = h.DB.QueryRow(
		`SELECT reward_coin, is_repeatable, is_active FROM activities WHERE activity_id = $1`,
		activityID,
	).Scan(&rewardCoin, &isRepeatable, &isActive)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบกิจกรรมนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !isActive {
		return writeError(c, fiber.StatusBadRequest, "กิจกรรมนี้ปิดใช้งานแล้ว")
	}

	var alreadyClaimed bool
	if isRepeatable {
		err = h.DB.QueryRow(
			`SELECT EXISTS(
				SELECT 1 FROM user_activities
				WHERE user_id = $1 AND activity_id = $2 AND completed_at::date = CURRENT_DATE
			)`,
			userID, activityID,
		).Scan(&alreadyClaimed)
	} else {
		err = h.DB.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM user_activities WHERE user_id = $1 AND activity_id = $2)`,
			userID, activityID,
		).Scan(&alreadyClaimed)
	}
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if alreadyClaimed {
		return writeError(c, fiber.StatusConflict, "คุณได้เข้าร่วมกิจกรรมนี้ไปแล้ว หรือไม่ตรงตามเงื่อนไข")
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	// ระบุ completed_at = NOW() ตรง ๆ ตอน insert เสมอ เดิมโค้ดปล่อยให้คอลัมน์นี้ใช้ค่า default ของตาราง
	// (ซึ่งดูเหมือนจะไม่มี หรือ default เป็น NULL) ทำให้เช็ค "completed_at::date = CURRENT_DATE" ด้านบน
	// เทียบกับ NULL แล้วได้ false เสมอ (ใน SQL, NULL = อะไรก็ตามจะไม่มีวันเป็น true) เลยเหมือนกับไม่เคยรับรางวัล
	// มาก่อนเลยสักครั้ง กดรับกี่รอบก็ผ่านเงื่อนไข "ยังไม่เคยรับวันนี้" ตลอด เป็นสาเหตุที่กดรับรางวัลได้ไม่จำกัด
	if _, err := tx.Exec(
		`INSERT INTO user_activities (user_id, activity_id, completed_at) VALUES ($1, $2, NOW())`,
		userID, activityID,
	); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ทำกิจกรรมไม่สำเร็จ")
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 RETURNING balance`,
		rewardCoin, userID,
	).Scan(&newBalance)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อัปเดตเหรียญไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ทำกิจกรรมไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{
		"message":     "รับรางวัลสำเร็จ",
		"reward_coin": rewardCoin,
		"new_balance": newBalance,
	})
}
