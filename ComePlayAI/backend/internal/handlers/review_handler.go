package handlers

import (
	"database/sql"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type ReviewHandler struct {
	DB *sql.DB
}

func NewReviewHandler(db *sql.DB) *ReviewHandler {
	return &ReviewHandler{DB: db}
}

type reviewRequest struct {
	Rating  int     `json:"rating"`
	Comment *string `json:"comment"`
}

// CreateOrUpdate สร้างรีวิวใหม่ หรืออัปเดตรีวิวเดิมถ้าเคยรีวิวตัวละครนี้ไปแล้ว (UPSERT)
func (h *ReviewHandler) CreateOrUpdate(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var req reviewRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}
	if req.Rating < 1 || req.Rating > 5 {
		return writeError(c, fiber.StatusBadRequest, "คะแนนต้องอยู่ระหว่าง 1-5")
	}

	var exists bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM characters WHERE character_id = $1)`, characterID).Scan(&exists); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !exists {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	}

	var review models.Review
	var comment sql.NullString
	err = h.DB.QueryRow(
		`INSERT INTO character_reviews (character_id, user_id, rating, comment)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (character_id, user_id)
		 DO UPDATE SET rating = EXCLUDED.rating, comment = EXCLUDED.comment
		 RETURNING review_id, character_id, user_id, rating, comment, created_at, updated_at`,
		characterID, userID, req.Rating, req.Comment,
	).Scan(&review.ReviewID, &review.CharacterID, &review.UserID, &review.Rating, &comment, &review.CreatedAt, &review.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกรีวิวไม่สำเร็จ")
	}
	if comment.Valid {
		review.Comment = &comment.String
	}

	_ = h.DB.QueryRow(`SELECT username FROM users WHERE user_id = $1`, userID).Scan(&review.Username)

	return writeJSON(c, fiber.StatusOK, review)
}

// List แสดงรีวิวทั้งหมดของตัวละคร (ไม่ต้องล็อกอินก็ดูได้)
func (h *ReviewHandler) List(c *fiber.Ctx) error {
	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	rows, err := h.DB.Query(
		`SELECT cr.review_id, cr.character_id, cr.user_id, u.username, cr.rating, cr.comment, cr.created_at, cr.updated_at
		 FROM character_reviews cr
		 JOIN users u ON u.user_id = cr.user_id
		 WHERE cr.character_id = $1
		 ORDER BY cr.created_at DESC`,
		characterID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรีวิวไม่สำเร็จ")
	}
	defer rows.Close()

	reviews := []models.Review{}
	for rows.Next() {
		var rv models.Review
		var comment sql.NullString
		if err := rows.Scan(&rv.ReviewID, &rv.CharacterID, &rv.UserID, &rv.Username, &rv.Rating, &comment, &rv.CreatedAt, &rv.UpdatedAt); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรีวิวไม่สำเร็จ")
		}
		if comment.Valid {
			rv.Comment = &comment.String
		}
		reviews = append(reviews, rv)
	}

	return writeJSON(c, fiber.StatusOK, reviews)
}
