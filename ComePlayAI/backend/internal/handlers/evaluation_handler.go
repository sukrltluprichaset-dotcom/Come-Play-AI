package handlers

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type EvaluationHandler struct {
	DB *sql.DB
}

func NewEvaluationHandler(db *sql.DB) *EvaluationHandler {
	return &EvaluationHandler{DB: db}
}

type evaluationAnswer struct {
	Question string `json:"question"`
	Answer   int    `json:"answer"`
}

type submitEvaluationRequest struct {
	// เดิมไม่มีฟิลด์นี้เลย ทั้งที่หน้าเว็บ (submitQuestionnaire ใน index.html) ส่ง character_id
	// มาด้วยทุกครั้งตอนกด "ส่งแบบประเมิน" — Go แค่เพิกเฉยฟิลด์ที่ struct ไม่รู้จักตอน BodyParser
	// เลยไม่ error ให้เห็น แต่คำตอบที่บันทึกไว้ในตาราง evaluations เลยไม่รู้เลยว่าเป็นของตัวละครไหน
	// รู้แค่ว่า user คนไหนตอบเท่านั้น แก้โดยรับค่านี้เข้ามาจริง ๆ แล้วบันทึกลง DB ด้วย
	CharacterID int64              `json:"character_id"`
	Answers     []evaluationAnswer `json:"answers"`
}

// Submit บันทึกคำตอบแบบประเมินหลายข้อพร้อมกันในครั้งเดียว
func (h *EvaluationHandler) Submit(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req submitEvaluationRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	if req.CharacterID <= 0 {
		return writeError(c, fiber.StatusBadRequest, "ไม่พบตัวละครที่จะประเมิน")
	}
	var characterExists bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM characters WHERE character_id = $1)`, req.CharacterID).Scan(&characterExists); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !characterExists {
		return writeError(c, fiber.StatusBadRequest, "ไม่พบตัวละครนี้ในระบบ")
	}

	if len(req.Answers) == 0 {
		return writeError(c, fiber.StatusBadRequest, "กรุณาตอบแบบประเมินให้ครบถ้วน")
	}
	for _, a := range req.Answers {
		if strings.TrimSpace(a.Question) == "" {
			return writeError(c, fiber.StatusBadRequest, "กรุณากรอกแบบประเมินให้ครบถ้วน")
		}
		if a.Answer < 1 || a.Answer > 5 {
			return writeError(c, fiber.StatusBadRequest, "คะแนนต้องอยู่ระหว่าง 1-5")
		}
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	saved := []models.Evaluation{}
	for _, a := range req.Answers {
		var ev models.Evaluation
		err = tx.QueryRow(
			`INSERT INTO evaluations (question, answer, user_id, character_id)
			 VALUES ($1, $2, $3, $4)
			 RETURNING eval_id, question, answer, user_id, character_id, created_at`,
			strings.TrimSpace(a.Question), a.Answer, userID, req.CharacterID,
		).Scan(&ev.EvalID, &ev.Question, &ev.Answer, &ev.UserID, &ev.CharacterID, &ev.CreatedAt)
		if err != nil {
			return writeError(c, fiber.StatusInternalServerError, "บันทึกแบบประเมินไม่สำเร็จ")
		}
		saved = append(saved, ev)
	}

	// บันทึกว่า user คนนี้ตอบแบบประเมินของตัวละครนี้ไปแล้วจริง (answered = true)
	// กัน popup ชวนตอบแบบประเมินเด้งถามซ้ำตัวละครเดิมอีกในครั้งต่อไป
	if _, err := tx.Exec(
		`INSERT INTO evaluation_prompts (user_id, character_id, answered)
		 VALUES ($1, $2, true)
		 ON CONFLICT (user_id, character_id) DO UPDATE SET answered = true`,
		userID, req.CharacterID,
	); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกแบบประเมินไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกแบบประเมินไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, saved)
}

// PromptStatus บอกฝั่งหน้าเว็บว่าเคยถาม/ตอบแบบประเมินของตัวละครนี้ไปแล้วหรือยัง
// (เรียกก่อนจะเด้ง popup ชวนตอบแบบประเมินตอนออกจากห้องแชท กันถามซ้ำตัวละครเดิม)
func (h *EvaluationHandler) PromptStatus(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var alreadyAsked bool
	err = h.DB.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM evaluation_prompts WHERE user_id = $1 AND character_id = $2)`,
		userID, characterID,
	).Scan(&alreadyAsked)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"already_asked": alreadyAsked})
}

// DismissPrompt บันทึกว่า user กดข้าม/ปิด popup ชวนตอบแบบประเมินของตัวละครนี้ไปแล้ว
// (answered = false เพราะแค่กดข้าม ไม่ได้ตอบจริง) กันไม่ให้เด้งถามซ้ำตัวละครเดิมอีก
func (h *EvaluationHandler) DismissPrompt(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	if _, err := h.DB.Exec(
		`INSERT INTO evaluation_prompts (user_id, character_id, answered)
		 VALUES ($1, $2, false)
		 ON CONFLICT (user_id, character_id) DO NOTHING`,
		userID, characterID,
	); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}
