package handlers

import (
	"database/sql"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/llm"
	"comeplayai-backend/internal/models"
)

type DiaryHandler struct {
	DB     *sql.DB
	Gemini *llm.GeminiClient
}

func NewDiaryHandler(db *sql.DB, gemini *llm.GeminiClient) *DiaryHandler {
	return &DiaryHandler{DB: db, Gemini: gemini}
}

// Generate สรุปบทสนทนา "วันนี้" ของผู้ใช้กับตัวละครนี้ เป็นบันทึกประจำวัน (สร้างใหม่ หรืออัปเดตทับถ้ามีอยู่แล้ว)
func (h *DiaryHandler) Generate(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var characterName, personality string
	var isShared bool
	var ownerID int64
	err = h.DB.QueryRow(
		`SELECT name, personality, is_shared, user_id FROM characters WHERE character_id = $1`,
		characterID,
	).Scan(&characterName, &personality, &isShared, &ownerID)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !isShared && ownerID != userID {
		return writeError(c, fiber.StatusForbidden, "ไม่มีสิทธิ์เข้าถึงตัวละครนี้")
	}

	rows, err := h.DB.Query(
		// เพิ่ม chat_id เป็นตัวตัดสินลำดับรอง กันเคส send_time เท่ากันเป๊ะ (ข้อความ user/ai ที่ insert ใน
		// ทรานแซกชันเดียวกันจะได้ send_time เท่ากัน) ทำให้เรียงบทสนทนาผิดลำดับตอนสรุปไดอารี่ (ดูรายละเอียด
		// เพิ่มเติมที่คอมเมนต์ใน chat_handler.go)
		`SELECT sender_type, message FROM chats
		 WHERE user_id = $1 AND character_id = $2 AND send_time::date = CURRENT_DATE
		 ORDER BY send_time ASC, chat_id ASC`,
		userID, characterID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดบทสนทนาไม่สำเร็จ")
	}
	var sb strings.Builder
	count := 0
	for rows.Next() {
		var senderType, message string
		if err := rows.Scan(&senderType, &message); err != nil {
			rows.Close()
			return writeError(c, fiber.StatusInternalServerError, "โหลดบทสนทนาไม่สำเร็จ")
		}
		speaker := "ผู้ใช้"
		if senderType == "ai" {
			speaker = characterName
		}
		sb.WriteString(speaker + ": " + message + "\n")
		count++
	}
	rows.Close()

	if count == 0 {
		return writeError(c, fiber.StatusBadRequest, "ยังไม่มีบทสนทนาในวันนี้ ไม่สามารถสร้างบันทึกได้")
	}

	summary, err := h.Gemini.SummarizeDiary(characterName, personality, sb.String())
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สรุปบันทึกไม่สำเร็จ กรุณาลองใหม่อีกครั้ง")
	}

	var existingID int64
	err = h.DB.QueryRow(
		`SELECT diary_id FROM diaries WHERE user_id = $1 AND character_id = $2 AND entry_date = CURRENT_DATE`,
		userID, characterID,
	).Scan(&existingID)

	var diary models.Diary
	if err == sql.ErrNoRows {
		err = h.DB.QueryRow(
			`INSERT INTO diaries (user_id, character_id, entry_date, summary)
			 VALUES ($1, $2, CURRENT_DATE, $3)
			 RETURNING diary_id, user_id, character_id, entry_date, summary, created_at`,
			userID, characterID, summary,
		).Scan(&diary.DiaryID, &diary.UserID, &diary.CharacterID, &diary.EntryDate, &diary.Summary, &diary.CreatedAt)
	} else if err == nil {
		err = h.DB.QueryRow(
			`UPDATE diaries SET summary = $1 WHERE diary_id = $2
			 RETURNING diary_id, user_id, character_id, entry_date, summary, created_at`,
			summary, existingID,
		).Scan(&diary.DiaryID, &diary.UserID, &diary.CharacterID, &diary.EntryDate, &diary.Summary, &diary.CreatedAt)
	}
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "บันทึกไดอารี่ไม่สำเร็จ")
	}
	diary.CharacterName = characterName

	return writeJSON(c, fiber.StatusOK, diary)
}

// List แสดงบันทึกทั้งหมดของผู้ใช้ (ทุกตัวละคร) เรียงตามวันที่ล่าสุด
func (h *DiaryHandler) List(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	rows, err := h.DB.Query(
		`SELECT d.diary_id, d.user_id, d.character_id, c.name, d.entry_date, d.summary, d.created_at
		 FROM diaries d
		 JOIN characters c ON c.character_id = d.character_id
		 WHERE d.user_id = $1
		 ORDER BY d.entry_date DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดบันทึกไม่สำเร็จ")
	}
	defer rows.Close()

	diaries := []models.Diary{}
	for rows.Next() {
		var d models.Diary
		if err := rows.Scan(&d.DiaryID, &d.UserID, &d.CharacterID, &d.CharacterName, &d.EntryDate, &d.Summary, &d.CreatedAt); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดบันทึกไม่สำเร็จ")
		}
		diaries = append(diaries, d)
	}

	return writeJSON(c, fiber.StatusOK, diaries)
}
