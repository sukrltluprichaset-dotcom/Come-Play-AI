package handlers

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/middleware"
	"comeplayai-backend/internal/models"
)

type CharacterHandler struct {
	DB *sql.DB
}

func NewCharacterHandler(db *sql.DB) *CharacterHandler {
	return &CharacterHandler{DB: db}
}

// scanner คือ interface กลางของ *sql.Row และ *sql.Rows (ทั้งคู่มีเมธอด Scan เหมือนกัน)
// ทำให้ใช้ฟังก์ชัน scanCharacter ตัวเดียวได้ทั้งตอน query แถวเดียวและหลายแถว
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanCharacter(row scanner) (models.Character, error) {
	var c models.Character
	var avatar sql.NullString

	err := row.Scan(
		&c.CharacterID, &c.Name, &c.Personality, &avatar,
		&c.UsageCount, &c.Rating, &c.ReviewCount, &c.IsShared,
		&c.UserID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return c, err
	}
	if avatar.Valid {
		c.AvatarURL = &avatar.String
	}
	return c, nil
}

func userIDFromContext(c *fiber.Ctx) int64 {
	userID, _ := c.Locals(middleware.UserIDKey).(int64)
	return userID
}

const characterColumns = `character_id, name, personality, avatar_url, usage_count, rating, review_count, is_shared, user_id, created_at, updated_at`

// ----- Create -----

type characterRequest struct {
	Name        string  `json:"name"`
	Personality string  `json:"personality"`
	AvatarURL   *string `json:"avatar_url"`
	IsShared    bool    `json:"is_shared"`
}

func (h *CharacterHandler) Create(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req characterRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Personality = strings.TrimSpace(req.Personality)

	if req.Name == "" || len(req.Name) > 100 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อตัวละครต้องไม่ว่างและไม่เกิน 100 ตัวอักษร")
	}
	if req.Personality == "" {
		return writeError(c, fiber.StatusBadRequest, "กรุณากรอกบุคลิกนิสัยของตัวละคร")
	}

	row := h.DB.QueryRow(
		`INSERT INTO characters (name, personality, avatar_url, is_shared, user_id)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+characterColumns,
		req.Name, req.Personality, req.AvatarURL, req.IsShared, userID,
	)

	character, err := scanCharacter(row)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สร้างตัวละครไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, character)
}

// ----- List (เฉพาะตัวละครของตัวเอง) -----

func (h *CharacterHandler) ListMine(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	rows, err := h.DB.Query(
		`SELECT `+characterColumns+` FROM characters WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายการตัวละครไม่สำเร็จ")
	}
	defer rows.Close()

	characters := []models.Character{}
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายการตัวละครไม่สำเร็จ")
		}
		characters = append(characters, ch)
	}

	return writeJSON(c, fiber.StatusOK, characters)
}

// ----- Get by ID -----

func (h *CharacterHandler) Get(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	row := h.DB.QueryRow(`SELECT `+characterColumns+` FROM characters WHERE character_id = $1`, id)
	character, err := scanCharacter(row)

	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	userID := userIDFromContext(c)
	if !character.IsShared && character.UserID != userID {
		return writeError(c, fiber.StatusForbidden, "ไม่มีสิทธิ์เข้าถึงตัวละครนี้")
	}

	return writeJSON(c, fiber.StatusOK, character)
}

// ----- Update (เฉพาะเจ้าของ) -----

func (h *CharacterHandler) Update(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var req characterRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Personality = strings.TrimSpace(req.Personality)

	if req.Name == "" || len(req.Name) > 100 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อตัวละครต้องไม่ว่างและไม่เกิน 100 ตัวอักษร")
	}
	if req.Personality == "" {
		return writeError(c, fiber.StatusBadRequest, "กรุณากรอกบุคลิกนิสัยของตัวละคร")
	}

	row := h.DB.QueryRow(
		`UPDATE characters
		 SET name = $1, personality = $2, avatar_url = $3, is_shared = $4
		 WHERE character_id = $5 AND user_id = $6
		 RETURNING `+characterColumns,
		req.Name, req.Personality, req.AvatarURL, req.IsShared, id, userID,
	)

	character, err := scanCharacter(row)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้ หรือคุณไม่ใช่เจ้าของ")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "แก้ไขตัวละครไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, character)
}

// ----- Delete (เฉพาะเจ้าของ) -----

func (h *CharacterHandler) Delete(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	result, err := h.DB.Exec(`DELETE FROM characters WHERE character_id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ลบตัวละครไม่สำเร็จ")
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้ หรือคุณไม่ใช่เจ้าของ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบตัวละครสำเร็จ"})
}

// ----- List Public (ค้นหาตัวละครสาธารณะของทุกคน ไม่ต้องล็อกอิน) -----

func (h *CharacterHandler) ListPublic(c *fiber.Ctx) error {
	query := strings.TrimSpace(c.Query("q"))
	sortBy := c.Query("sort")

	orderClause := "created_at DESC"
	switch sortBy {
	case "popular":
		orderClause = "usage_count DESC"
	case "rating":
		orderClause = "rating DESC"
	}

	var rows *sql.Rows
	var err error

	if query != "" {
		rows, err = h.DB.Query(
			`SELECT `+characterColumns+` FROM characters WHERE is_shared = true AND name ILIKE $1 ORDER BY `+orderClause,
			"%"+query+"%",
		)
	} else {
		rows, err = h.DB.Query(
			`SELECT ` + characterColumns + ` FROM characters WHERE is_shared = true ORDER BY ` + orderClause,
		)
	}
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ค้นหาตัวละครไม่สำเร็จ")
	}
	defer rows.Close()

	characters := []models.Character{}
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "ค้นหาตัวละครไม่สำเร็จ")
		}
		characters = append(characters, ch)
	}

	return writeJSON(c, fiber.StatusOK, characters)
}

// ----- Popular (ตัวละครยอดนิยม เรียงตามยอดใช้งาน) -----

func (h *CharacterHandler) Popular(c *fiber.Ctx) error {
	rows, err := h.DB.Query(
		`SELECT ` + characterColumns + ` FROM characters WHERE is_shared = true ORDER BY usage_count DESC LIMIT 10`,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายการตัวละครยอดนิยมไม่สำเร็จ")
	}
	defer rows.Close()

	characters := []models.Character{}
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายการตัวละครยอดนิยมไม่สำเร็จ")
		}
		characters = append(characters, ch)
	}

	return writeJSON(c, fiber.StatusOK, characters)
}

// ----- Stats (สถิติของตัวละครที่ตัวเองสร้าง เห็นได้เฉพาะเจ้าของ) -----

func (h *CharacterHandler) Stats(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	rows, err := h.DB.Query(
		`SELECT
			c.character_id,
			c.name,
			c.avatar_url,
			c.usage_count,
			(SELECT COUNT(DISTINCT ch.user_id) FROM chats ch WHERE ch.character_id = c.character_id AND ch.user_id != c.user_id) AS unique_chatters,
			c.rating,
			c.review_count
		 FROM characters c
		 WHERE c.user_id = $1
		 ORDER BY c.usage_count DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดสถิติไม่สำเร็จ")
	}
	defer rows.Close()

	stats := []models.CharacterStats{}
	for rows.Next() {
		var s models.CharacterStats
		var avatar sql.NullString
		if err := rows.Scan(&s.CharacterID, &s.Name, &avatar, &s.UsageCount, &s.UniqueChatters, &s.Rating, &s.ReviewCount); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดสถิติไม่สำเร็จ")
		}
		if avatar.Valid {
			s.AvatarURL = &avatar.String
		}
		stats = append(stats, s)
	}

	return writeJSON(c, fiber.StatusOK, stats)
}
