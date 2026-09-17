package handlers

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type PersonaHandler struct {
	DB *sql.DB
}

func NewPersonaHandler(db *sql.DB) *PersonaHandler {
	return &PersonaHandler{DB: db}
}

const personaColumns = `persona_id, user_id, name, description, is_active, created_at`

func scanPersona(row scanner) (models.Persona, error) {
	var p models.Persona
	err := row.Scan(&p.PersonaID, &p.UserID, &p.Name, &p.Description, &p.IsActive, &p.CreatedAt)
	return p, err
}

type personaRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ----- Create: สร้างบทบาทใหม่ (ค่าเริ่มต้นยังไม่ถูกเลือกใช้งาน ต้องกด "ใช้บทบาทนี้" อีกทีถึงจะ active) -----
func (h *PersonaHandler) Create(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req personaRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)

	if req.Name == "" || len(req.Name) > 50 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อบทบาทต้องไม่ว่างและไม่เกิน 50 ตัวอักษร")
	}
	if req.Description == "" || len(req.Description) > 300 {
		return writeError(c, fiber.StatusBadRequest, "คำอธิบายบทบาทต้องไม่ว่างและไม่เกิน 300 ตัวอักษร")
	}

	row := h.DB.QueryRow(
		`INSERT INTO user_personas (user_id, name, description, is_active)
		 VALUES ($1, $2, $3, false)
		 RETURNING `+personaColumns,
		userID, req.Name, req.Description,
	)

	persona, err := scanPersona(row)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สร้างบทบาทไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, persona)
}

// ----- List: บทบาททั้งหมดของผู้ใช้ เรียงล่าสุดก่อน -----
func (h *PersonaHandler) List(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	rows, err := h.DB.Query(
		`SELECT `+personaColumns+` FROM user_personas WHERE user_id = $1 ORDER BY created_at DESC, persona_id DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายการบทบาทไม่สำเร็จ")
	}
	defer rows.Close()

	personas := []models.Persona{}
	for rows.Next() {
		p, err := scanPersona(rows)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายการบทบาทไม่สำเร็จ")
		}
		personas = append(personas, p)
	}

	return writeJSON(c, fiber.StatusOK, personas)
}

// ----- Update: แก้ไขชื่อ/คำอธิบายบทบาท (เฉพาะเจ้าของ) -----
func (h *PersonaHandler) Update(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสบทบาทไม่ถูกต้อง")
	}

	var req personaRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)

	if req.Name == "" || len(req.Name) > 50 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อบทบาทต้องไม่ว่างและไม่เกิน 50 ตัวอักษร")
	}
	if req.Description == "" || len(req.Description) > 300 {
		return writeError(c, fiber.StatusBadRequest, "คำอธิบายบทบาทต้องไม่ว่างและไม่เกิน 300 ตัวอักษร")
	}

	row := h.DB.QueryRow(
		`UPDATE user_personas SET name = $1, description = $2
		 WHERE persona_id = $3 AND user_id = $4
		 RETURNING `+personaColumns,
		req.Name, req.Description, id, userID,
	)

	persona, err := scanPersona(row)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบบทบาทนี้ หรือคุณไม่ใช่เจ้าของ")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "แก้ไขบทบาทไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, persona)
}

// ----- Delete: ลบบทบาท (เฉพาะเจ้าของ) -----
func (h *PersonaHandler) Delete(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสบทบาทไม่ถูกต้อง")
	}

	result, err := h.DB.Exec(`DELETE FROM user_personas WHERE persona_id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ลบบทบาทไม่สำเร็จ")
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบบทบาทนี้ หรือคุณไม่ใช่เจ้าของ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบบทบาทสำเร็จ"})
}

// ----- Activate: เลือกใช้บทบาทนี้ (ปิดบทบาทอื่นของผู้ใช้คนเดียวกันทั้งหมดก่อนเสมอ ใช้งานได้ทีละบทบาท) -----
func (h *PersonaHandler) Activate(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสบทบาทไม่ถูกต้อง")
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	// ปิดบทบาทอื่นทั้งหมดของผู้ใช้ก่อน (กันชนกับ unique index ที่บังคับว่า active ได้ทีละ 1 บทบาทต่อผู้ใช้)
	if _, err := tx.Exec(`UPDATE user_personas SET is_active = false WHERE user_id = $1`, userID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เลือกใช้บทบาทไม่สำเร็จ")
	}

	result, err := tx.Exec(`UPDATE user_personas SET is_active = true WHERE persona_id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เลือกใช้บทบาทไม่สำเร็จ")
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบบทบาทนี้ หรือคุณไม่ใช่เจ้าของ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เลือกใช้บทบาทไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "เลือกใช้บทบาทนี้แล้ว"})
}

// ----- ClearActive: เลิกใช้บทบาททั้งหมด (กลับไปคุยแบบไม่มีบทบาทกำกับ) -----
func (h *PersonaHandler) ClearActive(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	if _, err := h.DB.Exec(`UPDATE user_personas SET is_active = false WHERE user_id = $1`, userID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เลิกใช้บทบาทไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "เลิกใช้บทบาทแล้ว"})
}
