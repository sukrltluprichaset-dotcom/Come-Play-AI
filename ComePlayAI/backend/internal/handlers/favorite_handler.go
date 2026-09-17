package handlers

import (
	"database/sql"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type FavoriteHandler struct {
	DB *sql.DB
}

func NewFavoriteHandler(db *sql.DB) *FavoriteHandler {
	return &FavoriteHandler{DB: db}
}

// ----- Add: เพิ่มตัวละครเข้ารายการที่ถูกใจ/จัดเก็บของผู้ใช้ -----
// กดซ้ำได้โดยไม่ error (ON CONFLICT DO NOTHING) กันเคสผู้ใช้กดปุ่มถี่ ๆ ซ้อนกัน
func (h *FavoriteHandler) Add(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var exists bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM characters WHERE character_id = $1)`, characterID).Scan(&exists); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !exists {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	}

	if _, err := h.DB.Exec(
		`INSERT INTO favorites (user_id, character_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, character_id) DO NOTHING`,
		userID, characterID,
	); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เพิ่มรายการถูกใจไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "เพิ่มในรายการถูกใจแล้ว"})
}

// ----- Remove: ลบตัวละครออกจากรายการที่ถูกใจ/จัดเก็บของผู้ใช้ -----
func (h *FavoriteHandler) Remove(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	if _, err := h.DB.Exec(`DELETE FROM favorites WHERE user_id = $1 AND character_id = $2`, userID, characterID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ลบรายการถูกใจไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบออกจากรายการถูกใจแล้ว"})
}

// ----- List: แสดงตัวละครทั้งหมดที่ผู้ใช้ถูกใจ/จัดเก็บไว้ เรียงจากล่าสุดก่อน -----
func (h *FavoriteHandler) List(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	rows, err := h.DB.Query(
		`SELECT ch.character_id, ch.name, ch.personality, ch.avatar_url, ch.usage_count,
		        ch.rating, ch.review_count, ch.is_shared, ch.user_id, ch.created_at, ch.updated_at
		 FROM favorites f
		 JOIN characters ch ON ch.character_id = f.character_id
		 WHERE f.user_id = $1
		 ORDER BY f.created_at DESC, f.favorite_id DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายการถูกใจไม่สำเร็จ")
	}
	defer rows.Close()

	characters := []models.Character{}
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายการถูกใจไม่สำเร็จ")
		}
		characters = append(characters, ch)
	}

	return writeJSON(c, fiber.StatusOK, characters)
}
