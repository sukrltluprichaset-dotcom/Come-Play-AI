package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"

	"comeplayai-backend/internal/llm"
	"comeplayai-backend/internal/models"
)

// ประเทศไทยใช้เวลา UTC+7 คงที่ตลอดปี ไม่มีปรับเวลาออมแสง (DST) จึงใช้ FixedZone ตรง ๆ ได้เลย
// ปลอดภัยกว่าการเรียก time.LoadLocation("Asia/Bangkok") ซึ่งอาจ error ได้ถ้าเซิร์ฟเวอร์ที่ deploy จริง
// (เช่น Render) ไม่มีฐานข้อมูล timezone (tzdata) ติดตั้งไว้ในอิมเมจ
var bangkokTZ = time.FixedZone("ICT", 7*60*60)

const chatCost = 2 // จำนวนเหรียญที่หักต่อการส่งข้อความ 1 ครั้ง (ตรงกับผลทดสอบตารางที่ 4.3 ในเล่ม)

type ChatHandler struct {
	DB     *sql.DB
	Gemini *llm.GeminiClient
}

func NewChatHandler(db *sql.DB, gemini *llm.GeminiClient) *ChatHandler {
	return &ChatHandler{DB: db, Gemini: gemini}
}

type sendMessageRequest struct {
	Message string `json:"message"`
}

// cosineSimilarity คำนวณความคล้ายคลึงเชิงความหมายระหว่างเวกเตอร์ 2 ตัว (ค่ายิ่งใกล้ 1 ยิ่งคล้ายกันมาก)
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func (h *ChatHandler) SendMessage(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var req sendMessageRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		return writeError(c, fiber.StatusBadRequest, "ข้อความห้ามว่างเปล่า")
	}

	var isShared bool
	var ownerID int64
	var personality string
	err = h.DB.QueryRow(
		`SELECT is_shared, user_id, personality FROM characters WHERE character_id = $1`,
		characterID,
	).Scan(&isShared, &ownerID, &personality)

	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !isShared && ownerID != userID {
		return writeError(c, fiber.StatusForbidden, "ไม่มีสิทธิ์คุยกับตัวละครนี้")
	}

	var currentBalance int
	if err := h.DB.QueryRow(`SELECT balance FROM coins WHERE user_id = $1`, userID).Scan(&currentBalance); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if currentBalance < chatCost {
		return writeError(c, fiber.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
	}

	// ----- ดึงความจำระยะสั้น: 20 ข้อความล่าสุดในห้องนี้ -----
	// เดิมดึงมาแค่ sender_type, message ไม่มีเวลาที่พิมพ์กำกับไปด้วยเลย ทำให้ AI ไม่รู้เลยว่าแต่ละข้อความ
	// ในประวัติห่างจากตอนนี้นานแค่ไหน (ฟีดแบ็กผู้ใช้จริง: บางทีเอาเรื่องที่คุยเมื่อวานมาพูดถึงกับ AI วันนี้
	// แต่ AI ตอบราวกับเพิ่งเกิดขึ้นเมื่อกี้ ทั้งที่จริงผ่านไปเป็นวันแล้ว) เพิ่ม send_time เข้ามาด้วยเพื่อ
	// พิมพ์กำกับวันที่/เวลาไว้หน้าแต่ละข้อความในประวัติ ให้โมเดลใช้เทียบกับเวลาปัจจุบันเองได้
	// เจอบั๊กจากการทดสอบจริง: ข้อความของผู้ใช้กับคำตอบของ AI ที่ insert ในทรานแซกชันเดียวกัน (ดูจุด insert
	// ด้านล่าง) จะได้ค่า send_time เท่ากันเป๊ะเสมอ (Postgres: now()/CURRENT_TIMESTAMP คืนเวลาเริ่มทรานแซกชัน
	// เดียวกันตลอดทั้งทรานแซกชัน ไม่ใช่เวลา ณ ตอน insert แต่ละคำสั่งจริง ๆ) พอเวลาเท่ากันเป๊ะ ORDER BY
	// send_time เพียงอย่างเดียวจึงเรียงลำดับไม่แน่นอน (Postgres สุ่มลำดับแถวที่ค่าเท่ากันได้) บางทีเลยได้
	// คำตอบ AI โผล่มาก่อนข้อความของผู้ใช้ที่ถามจริง ๆ เพิ่ม chat_id (auto-increment ตามลำดับ insert จริง)
	// เป็นตัวตัดสินลำดับรอง ให้เรียงถูกต้องเสมอแม้ send_time จะชนกัน
	historyRows, err := h.DB.Query(
		`SELECT sender_type, message, send_time FROM (
			SELECT sender_type, message, send_time, chat_id FROM chats
			WHERE user_id = $1 AND character_id = $2
			ORDER BY send_time DESC, chat_id DESC LIMIT 20
		) recent ORDER BY send_time ASC, chat_id ASC`,
		userID, characterID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
	}
	var history []llm.ChatTurn
	for historyRows.Next() {
		var senderType, message string
		var sendTime time.Time
		if err := historyRows.Scan(&senderType, &message, &sendTime); err != nil {
			historyRows.Close()
			return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
		}
		role := "user"
		if senderType == "ai" {
			role = "model"
		}
		timestampedMessage := fmt.Sprintf("[%s] %s", sendTime.In(bangkokTZ).Format("02/01/2006 15:04"), message)
		history = append(history, llm.ChatTurn{Role: role, Text: timestampedMessage})
	}
	historyRows.Close()

	// เดิมเอาบุคลิก (personality) ที่ผู้ใช้พิมพ์ไว้ตอนสร้างตัวละครส่งให้ Gemini ตรง ๆ แบบไม่มีคำสั่งกำกับ
	// โทน/อารมณ์การพูดเลย พอผู้ใช้พิมพ์บุคลิกสั้น ๆ (เช่น "ร่าเริง สดใส") โมเดลเลยมักตอบแบบทางการ
	// ราบเรียบเหมือนแชทบอทบริการลูกค้า ไม่มีอารมณ์ความรู้สึกใด ๆ (ฟีดแบ็กผู้ใช้จริง: "เหมือนคุยกับหุ่นยนต์
	// ไม่มีอารมณ์อะไรตอนคุยเลย") เพิ่มคำสั่งสวมบทบาทกำกับไว้ก่อนบุคลิกของผู้ใช้เสมอ ให้ตอบแบบมีชีวิตชีวา
	// แสดงอารมณ์จริง ไม่ใช่พูดในมุมมอง AI ผู้ช่วย
	// อัปเดต: มีฟีดแบ็กเพิ่มว่าบางทีคำตอบยาวเกินไป อ่านเหมือนนิยายไม่เหมือนแชทคุยกัน เพิ่มกฎเรื่องความยาว
	// คำตอบให้ชัดเจนขึ้น (ปกติสั้น ๆ แบบข้อความแชทจริง ยาวขึ้นเฉพาะตอนจำเป็นจริง ๆ)
	// อัปเดตอีกครั้ง: มีฟีดแบ็กว่า AI ไม่รู้เรื่องเวลาเลย ผู้ใช้เอาเรื่องที่คุยเมื่อวานมาบ่น/พูดถึงต่อวันนี้
	// AI กลับตอบราวกับเพิ่งเกิดขึ้นเมื่อกี้ ทั้งที่ผ่านมาเป็นวันแล้ว เปลี่ยนจาก const เป็นสร้างข้อความแบบ
	// dynamic ด้วย fmt.Sprintf เพื่อแทรกเวลาปัจจุบัน (เทียบกับเวลาที่กำกับไว้หน้าแต่ละข้อความในประวัติ ดู
	// จุดที่ต่อ history ด้านบน) ให้โมเดลรับรู้ว่าเวลาผ่านไปนานแค่ไหนแล้วจริง ๆ
	// อัปเดตอีกครั้ง (เจอบั๊กจากการทดสอบจริง): คำสั่งเรื่องเวลารอบก่อนทำให้ AI "หลุดโฟกัส" ไปหยิบยกข้อความ
	// เก่าจากเมื่อวาน/ก่อนหน้าขึ้นมาตอบเอง แทนที่จะตอบข้อความล่าสุดที่ผู้ใช้เพิ่งพิมพ์มาจริง ๆ (เช่น ผู้ใช้ถาม
	// เรื่องคณิตศาสตร์ แต่ AI กลับย้อนไปพูดเรื่องเดิมที่คุยไปก่อนหน้าแทน) เพิ่มกฎ "ตอบข้อความล่าสุดเป็นหลัก
	// เสมอ" ไว้ก่อน และจำกัดว่าให้ใช้ข้อมูลเวลาเป็นแค่บริบทเสริมเวลาตีความข้อความล่าสุดเท่านั้น ห้ามใช้เป็น
	// ข้ออ้างไปหยิบยกเรื่องเก่าขึ้นมาพูดเองโดยผู้ใช้ไม่ได้เอ่ยถึงก่อน
	roleplayGuide := fmt.Sprintf(`คุณกำลังสวมบทบาทเป็นตัวละครในแอปแชท ไม่ใช่ผู้ช่วย AI ทั่วไป ให้ยึดบุคลิกที่กำหนดไว้ด้านล่างอย่างเคร่งครัด และทำตามกฎการพูดคุยต่อไปนี้เสมอ:
- กฎสำคัญที่สุด: ตอบสนองกับ "ข้อความล่าสุด" ที่ผู้ใช้เพิ่งพิมพ์มาท้ายสุดเป็นหลักเสมอ ห้ามข้ามไปหยิบยกหรือ "ตอบ" ข้อความเก่าในประวัติสนทนาขึ้นมาเองโดยผู้ใช้ไม่ได้พูดถึงมันในข้อความล่าสุด แม้ข้อความเก่านั้นจะดูน่าสนใจ/ตลก/มีดราม่าแค่ไหนก็ตาม
- ตอบในมุมมองบุคคลที่หนึ่งของตัวละคร (พูดแทนตัวเอง) ห้ามพูดถึงตัวเองว่าเป็น AI/โมเดลภาษา/ผู้ช่วย และห้ามใช้ประโยคทางการแบบบริการลูกค้า เช่น "ยินดีให้บริการค่ะ" หรือ "มีอะไรให้ช่วยเหลือเพิ่มเติมไหมคะ"
- แสดงอารมณ์ความรู้สึกออกมาให้เป็นธรรมชาติตามบุคลิกของตัวละคร (ดีใจ งอน หยอกล้อ ห่วงใย ตื่นเต้น ฯลฯ) ไม่ใช่ตอบแบบราบเรียบไร้ความรู้สึก
- ใช้ภาษาพูดที่เป็นธรรมชาติแบบคนคุยกันจริง ไม่ใช่ภาษาเขียนทางการ
- ตอบสั้นกระชับแบบข้อความแชทจริง ๆ ปกติแค่ 1-3 ประโยคต่อครั้งก็พอ ห้ามบรรยายฉาก ท่าทาง หรือใช้โวหารพรรณนายืดยาวแบบย่อหน้าในนิยาย เว้นแต่ผู้ใช้ถามคำถามที่ต้องอธิบายละเอียดจริง ๆ หรือขอให้เล่าเรื่องยาว ๆ เป็นพิเศษ ค่อยตอบยาวขึ้นเท่าที่จำเป็น
- ตอนนี้คือวันที่/เวลา %s (เขตเวลาไทย) ข้อความแต่ละอันในประวัติสนทนาด้านล่างจะมี [วันที่ เวลา] กำกับไว้หน้าข้อความ ใช้ข้อมูลนี้เป็นแค่บริบทเสริมเพื่อเข้าใจสิ่งที่ผู้ใช้เพิ่งพิมพ์มาล่าสุดให้ถูกต้องเท่านั้น (เช่น ถ้าผู้ใช้เอ่ยถึงเรื่องที่คุยไปเมื่อวานเอง ให้รับรู้ว่ามันคือเมื่อวานจริง ไม่ใช่เพิ่งเกิดขึ้น) ห้ามใช้ข้อมูลเวลานี้เป็นข้ออ้างไปหยิบยกเรื่องเก่าขึ้นมาพูดเองโดยผู้ใช้ไม่ได้เอ่ยถึงก่อน

บุคลิกของตัวละครที่คุณต้องสวมบทบาท:
`, time.Now().In(bangkokTZ).Format("02/01/2006 15:04"))

	// ----- RAG เฟส 2: ค้นหาความจำระยะยาวที่เกี่ยวข้อง (นอกเหนือจาก 20 ข้อความล่าสุด) -----
	fullPersonality := roleplayGuide + personality

	userEmbedding, embedErr := h.Gemini.EmbedText(req.Message)
	if embedErr != nil {
		log.Printf("Embedding error (ข้ามการค้นหาความจำระยะยาวรอบนี้): %v", embedErr)
	} else {
		type oldMsg struct {
			Message   string
			Embedding []float64
		}
		oldRows, err := h.DB.Query(
			`SELECT message, embedding FROM chats
			 WHERE user_id = $1 AND character_id = $2 AND embedding IS NOT NULL
			 ORDER BY send_time DESC, chat_id DESC OFFSET 20 LIMIT 300`,
			userID, characterID,
		)
		if err == nil {
			var candidates []oldMsg
			for oldRows.Next() {
				var m oldMsg
				if err := oldRows.Scan(&m.Message, pq.Array(&m.Embedding)); err == nil {
					candidates = append(candidates, m)
				}
			}
			oldRows.Close()

			type scored struct {
				Message string
				Score   float64
			}
			var scoredList []scored
			for _, cand := range candidates {
				score := cosineSimilarity(userEmbedding, cand.Embedding)
				if score > 0.75 {
					scoredList = append(scoredList, scored{Message: cand.Message, Score: score})
				}
			}
			sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].Score > scoredList[j].Score })

			if len(scoredList) > 0 {
				limit := 5
				if len(scoredList) < limit {
					limit = len(scoredList)
				}
				var sb strings.Builder
				sb.WriteString("\n\n[ความทรงจำเก่าที่เกี่ยวข้องกับสิ่งที่ผู้ใช้เพิ่งพูดถึง]\n")
				for _, s := range scoredList[:limit] {
					sb.WriteString("- " + s.Message + "\n")
				}
				fullPersonality += sb.String()
			}
		}
	}

	aiReply, err := h.Gemini.GenerateReply(fullPersonality, history, req.Message)
	if err != nil {
		log.Printf("Gemini API error: %v", err)
		// เดิมพอ Gemini ปฏิเสธเพราะตัวกรองความปลอดภัย (เช่นข้อความมีคำหยาบ) จะโชว์ข้อความ
		// "ระบบ AI ขัดข้อง กรุณาลองใหม่อีกครั้ง" เหมือนกับตอนระบบล่มจริง ๆ ทำให้ผู้ใช้เข้าใจผิดว่ากดลองใหม่
		// แล้วจะสำเร็จ ทั้งที่พิมพ์ข้อความเดิมซ้ำยังไงก็โดนบล็อกซ้ำแน่นอน แยกเคสนี้ออกมาบอกสาเหตุจริงแทน
		if errors.Is(err, llm.ErrContentBlocked) {
			return writeError(c, fiber.StatusBadRequest, "ข้อความนี้มีเนื้อหาที่ไม่เหมาะสม ระบบ AI ไม่สามารถตอบกลับได้ กรุณาลองพิมพ์ข้อความอื่น")
		}
		return writeError(c, fiber.StatusInternalServerError, "ระบบ AI ขัดข้อง กรุณาลองใหม่อีกครั้ง")
	}

	aiEmbedding, embedErr := h.Gemini.EmbedText(aiReply)
	if embedErr != nil {
		log.Printf("Embedding error (คำตอบ AI): %v", embedErr)
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	var userChat models.Chat
	err = tx.QueryRow(
		`INSERT INTO chats (sender_type, message, user_id, character_id, embedding)
		 VALUES ('user', $1, $2, $3, $4)
		 RETURNING chat_id, sender_type, message, send_time, user_id, character_id`,
		req.Message, userID, characterID, pq.Array(userEmbedding),
	).Scan(&userChat.ChatID, &userChat.SenderType, &userChat.Message, &userChat.SendTime, &userChat.UserID, &userChat.CharacterID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ส่งข้อความไม่สำเร็จ")
	}

	var aiChat models.Chat
	err = tx.QueryRow(
		`INSERT INTO chats (sender_type, message, user_id, character_id, embedding)
		 VALUES ('ai', $1, $2, $3, $4)
		 RETURNING chat_id, sender_type, message, send_time, user_id, character_id`,
		aiReply, userID, characterID, pq.Array(aiEmbedding),
	).Scan(&aiChat.ChatID, &aiChat.SenderType, &aiChat.Message, &aiChat.SendTime, &aiChat.UserID, &aiChat.CharacterID)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "รับคำตอบไม่สำเร็จ")
	}

	if _, err := tx.Exec(`UPDATE characters SET usage_count = usage_count + 1 WHERE character_id = $1`, characterID); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อัปเดตสถิติไม่สำเร็จ")
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance - $1 WHERE user_id = $2 AND balance >= $1 RETURNING balance`,
		chatCost, userID,
	).Scan(&newBalance)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "หักเหรียญไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ส่งข้อความไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, fiber.Map{
		"user_message": userChat,
		"ai_message":   aiChat,
		"coin_spent":   chatCost,
		"coin_balance": newBalance,
	})
}

func (h *ChatHandler) GetHistory(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	characterID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	// เจอบั๊กจากการทดสอบจริง: ข้อความผู้ใช้กับคำตอบ AI ที่ insert ในทรานแซกชันเดียวกันได้ send_time เท่ากันเป๊ะ
	// (ดูคอมเมนต์อธิบายละเอียดที่จุดเดียวกันใน SendMessage ด้านบน) ORDER BY send_time อย่างเดียวเลยเรียง
	// ลำดับไม่แน่นอนเวลาเจอค่าเท่ากัน เพิ่ม chat_id เป็นตัวตัดสินลำดับรองให้เรียงถูกต้องเสมอ (endpoint นี้คือ
	// ตัวที่หน้าเว็บเรียกตอนเข้าห้องแชท ถ้าลำดับผิดตรงนี้ผู้ใช้จะเห็นคำตอบ AI โผล่มาก่อนข้อความที่ถามจริง)
	rows, err := h.DB.Query(
		`SELECT chat_id, sender_type, message, send_time, user_id, character_id
		 FROM chats WHERE user_id = $1 AND character_id = $2 ORDER BY send_time ASC, chat_id ASC`,
		userID, characterID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
	}
	defer rows.Close()

	chats := []models.Chat{}
	for rows.Next() {
		var ch models.Chat
		if err := rows.Scan(&ch.ChatID, &ch.SenderType, &ch.Message, &ch.SendTime, &ch.UserID, &ch.CharacterID); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
		}
		chats = append(chats, ch)
	}

	return writeJSON(c, fiber.StatusOK, chats)
}
