package handlers

import (
	"database/sql"
	"net/http"
	"strconv"

	"comeplayai-backend/internal/models"
)

type ActivityHandler struct {
	DB *sql.DB
}

func NewActivityHandler(db *sql.DB) *ActivityHandler {
	return &ActivityHandler{DB: db}
}

func (h *ActivityHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT activity_id, activity_name, description, reward_coin, is_repeatable
		 FROM activities WHERE is_active = true ORDER BY activity_id ASC`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดรายการกิจกรรมไม่สำเร็จ")
		return
	}
	defer rows.Close()

	activities := []models.Activity{}
	for rows.Next() {
		var a models.Activity
		var description sql.NullString
		if err := rows.Scan(&a.ActivityID, &a.ActivityName, &description, &a.RewardCoin, &a.IsRepeatable); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดรายการกิจกรรมไม่สำเร็จ")
			return
		}
		if description.Valid {
			a.Description = &description.String
		}
		activities = append(activities, a)
	}

	writeJSON(w, http.StatusOK, activities)
}

func (h *ActivityHandler) Claim(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	activityID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "รหัสกิจกรรมไม่ถูกต้อง")
		return
	}

	var rewardCoin int
	var isRepeatable, isActive bool
	err = h.DB.QueryRow(
		`SELECT reward_coin, is_repeatable, is_active FROM activities WHERE activity_id = $1`,
		activityID,
	).Scan(&rewardCoin, &isRepeatable, &isActive)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "ไม่พบกิจกรรมนี้")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	if !isActive {
		writeError(w, http.StatusBadRequest, "กิจกรรมนี้ปิดใช้งานแล้ว")
		return
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
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	if alreadyClaimed {
		writeError(w, http.StatusConflict, "คุณได้เข้าร่วมกิจกรรมนี้ไปแล้ว หรือไม่ตรงตามเงื่อนไข")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO user_activities (user_id, activity_id) VALUES ($1, $2)`,
		userID, activityID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "ทำกิจกรรมไม่สำเร็จ")
		return
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 RETURNING balance`,
		rewardCoin, userID,
	).Scan(&newBalance)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "อัปเดตเหรียญไม่สำเร็จ")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "ทำกิจกรรมไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "รับรางวัลสำเร็จ",
		"reward_coin": rewardCoin,
		"new_balance": newBalance,
	})
}
