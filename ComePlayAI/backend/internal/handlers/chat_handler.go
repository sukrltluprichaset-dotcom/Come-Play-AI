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
	"sync"
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

	// จับเวลาแต่ละขั้นไว้พิมพ์ลง log (ดูใน Render > Logs ได้ว่าช้าที่ขั้นไหน)
	tStart := time.Now()

	// ขั้นเตรียมข้อมูลทั้งหมดด้านล่างไม่ขึ้นต่อกัน จึงยิงพร้อมกันหมด (เดิมทำต่อกันทีละขั้น ทุกขั้นที่ต่อ
	// ฐานข้อมูลบนคลาวด์เสียเวลาไปกลับขั้นละหลายร้อยมิลลิวินาที รวมกันแล้วช้ามาก):
	//   1) แปลงข้อความผู้ใช้เป็นเวกเตอร์ (Gemini Embedding)
	//   2) เช็กยอดเหรียญ  3) ดึง 20 ข้อความล่าสุด  4) ดึงบทบาทผู้ใช้  5) ดึงข้อความเก่าที่มีเวกเตอร์ไว้ค้นความจำ
	// ข้อ 5 ดึงรอไว้ก่อนเลย พอได้เวกเตอร์จากข้อ 1 ค่อยเอามาคำนวณความคล้าย ไม่ต้องรอ DB อีกรอบ
	type embedResult struct {
		vec []float64
		err error
	}
	embedCh := make(chan embedResult, 1)
	go func() {
		v, e := h.Gemini.EmbedText(req.Message)
		embedCh <- embedResult{vec: v, err: e}
	}()

	type oldMsg struct {
		Message   string
		Embedding []float64
	}
	var (
		currentBalance                  int
		balErr                          error
		history                         []llm.ChatTurn
		histErr                         error
		personaName, personaDescription string
		personaErr                      error
		candidates                      []oldMsg
	)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		balErr = h.DB.QueryRow(`SELECT balance FROM coins WHERE user_id = $1`, userID).Scan(&currentBalance)
	}()
	go func() {
		defer wg.Done()
		// ความจำระยะสั้น: 20 ข้อความล่าสุดในห้องนี้ พร้อมเวลาที่พิมพ์ (ให้ AI รู้ว่าแต่ละข้อความห่างจากตอนนี้นานแค่ไหน)
		// เรียงด้วย send_time แล้วตามด้วย chat_id เพราะข้อความผู้ใช้กับคำตอบ AI ที่ insert ในทรานแซกชันเดียวกัน
		// ได้ send_time เท่ากันเป๊ะ ต้องใช้ chat_id ตัดสินลำดับรอง
		rows, err := h.DB.Query(
			`SELECT sender_type, message, send_time FROM (
				SELECT sender_type, message, send_time, chat_id FROM chats
				WHERE user_id = $1 AND character_id = $2
				ORDER BY send_time DESC, chat_id DESC LIMIT 20
			) recent ORDER BY send_time ASC, chat_id ASC`,
			userID, characterID,
		)
		if err != nil {
			histErr = err
			return
		}
		defer rows.Close()
		for rows.Next() {
			var senderType, message string
			var sendTime time.Time
			if err := rows.Scan(&senderType, &message, &sendTime); err != nil {
				histErr = err
				return
			}
			role := "user"
			if senderType == "ai" {
				role = "model"
			}
			timestampedMessage := fmt.Sprintf("[%s] %s", sendTime.In(bangkokTZ).Format("02/01/2006 15:04"), message)
			history = append(history, llm.ChatTurn{Role: role, Text: timestampedMessage})
		}
	}()
	go func() {
		defer wg.Done()
		// บทบาท/สถานการณ์ที่ผู้ใช้ตั้งไว้ (ใช้ได้ทีละ 1 บทบาท) ถ้าไม่มีก็ข้ามไป
		personaErr = h.DB.QueryRow(
			`SELECT name, description FROM user_personas WHERE user_id = $1 AND is_active = true LIMIT 1`,
			userID,
		).Scan(&personaName, &personaDescription)
	}()
	go func() {
		defer wg.Done()
		rows, err := h.DB.Query(
			`SELECT message, embedding FROM chats
			 WHERE user_id = $1 AND character_id = $2 AND embedding IS NOT NULL
			 ORDER BY send_time DESC, chat_id DESC OFFSET 20 LIMIT 120`,
			userID, characterID,
		)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var m oldMsg
			if err := rows.Scan(&m.Message, pq.Array(&m.Embedding)); err == nil {
				candidates = append(candidates, m)
			}
		}
	}()
	wg.Wait()
	tDB := time.Since(tStart)

	if balErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if currentBalance < chatCost {
		return writeError(c, fiber.StatusPaymentRequired, "เหรียญไม่เพียงพอ กรุณาเติมเหรียญก่อนแชท")
	}
	if histErr != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติการสนทนาไม่สำเร็จ")
	}

	personaSection := ""
	if personaErr == nil {
		personaSection = fmt.Sprintf("\n- ผู้ใช้ที่คุณกำลังคุยด้วยตอนนี้สวมบทบาทเป็น \"%s\" (%s) ให้คุณปฏิบัติและพูดคุยกับผู้ใช้ตามบทบาท/สถานการณ์นี้ตลอดการสนทนา", personaName, personaDescription)
	} else if personaErr != sql.ErrNoRows {
		log.Printf("โหลดบทบาทผู้ใช้ไม่สำเร็จ (ข้ามไปใช้ค่าเริ่มต้น): %v", personaErr)
	}

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
- ตอนนี้คือวันที่/เวลา %s (เขตเวลาไทย) ข้อความแต่ละอันในประวัติสนทนาด้านล่างจะมี [วันที่ เวลา] กำกับไว้หน้าข้อความ ใช้ข้อมูลนี้เป็นแค่บริบทเสริมเพื่อเข้าใจสิ่งที่ผู้ใช้เพิ่งพิมพ์มาล่าสุดให้ถูกต้องเท่านั้น (เช่น ถ้าผู้ใช้เอ่ยถึงเรื่องที่คุยไปเมื่อวานเอง ให้รับรู้ว่ามันคือเมื่อวานจริง ไม่ใช่เพิ่งเกิดขึ้น) ห้ามใช้ข้อมูลเวลานี้เป็นข้ออ้างไปหยิบยกเรื่องเก่าขึ้นมาพูดเองโดยผู้ใช้ไม่ได้เอ่ยถึงก่อน%s

บุคลิกของตัวละครที่คุณต้องสวมบทบาท:
`, time.Now().In(bangkokTZ).Format("02/01/2006 15:04"), personaSection)

	// ----- RAG เฟส 2: ค้นหาความจำระยะยาวที่เกี่ยวข้อง (นอกเหนือจาก 20 ข้อความล่าสุด) -----
	fullPersonality := roleplayGuide + personality

	// รอเวกเตอร์ได้ไม่เกิน 2 วินาที ถ้า Embedding ช้ากว่านั้นให้ข้ามการค้นความจำเก่ารอบนี้ไปก่อน
	// (แชทยังตอบได้ปกติ) แล้วค่อยเก็บเวกเตอร์ของข้อความนี้ตามหลังเป็นงานเบื้องหลัง
	var er embedResult
	lateEmbed := false
	select {
	case er = <-embedCh:
	case <-time.After(2 * time.Second):
		lateEmbed = true
		er = embedResult{err: errors.New("embedding ช้าเกิน 2 วินาที")}
	}
	userEmbedding, embedErr := er.vec, er.err
	tEmbed := time.Since(tStart)
	if embedErr != nil {
		log.Printf("Embedding error (ข้ามการค้นหาความจำระยะยาวรอบนี้): %v", embedErr)
	} else {
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

	tGen := time.Now()
	aiReply, err := h.Gemini.GenerateReply(fullPersonality, history, req.Message)
	genDur := time.Since(tGen)
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
		aiReply, userID, characterID, pq.Array([]float64(nil)),
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

	log.Printf("เวลาแชท: ดึงข้อมูล=%v | รอ embedding=%v | Gemini=%v | รวม=%v", tDB, tEmbed, genDur, time.Since(tStart))

	// ถ้ารอ embedding ไม่ทันตอนบันทึก ให้รอให้เสร็จเบื้องหลังแล้วเติมเวกเตอร์ของข้อความผู้ใช้ภายหลัง
	if lateEmbed {
		userChatID := userChat.ChatID
		go func() {
			r := <-embedCh
			if r.err != nil {
				log.Printf("Embedding error (ข้อความผู้ใช้ ทำตามหลัง): %v", r.err)
				return
			}
			if _, err := h.DB.Exec(`UPDATE chats SET embedding = $1 WHERE chat_id = $2`, pq.Array(r.vec), userChatID); err != nil {
				log.Printf("บันทึก embedding ข้อความผู้ใช้ไม่สำเร็จ: %v", err)
			}
		}()
	}

	// แปลงคำตอบ AI เป็นเวกเตอร์เก็บไว้ค้นความจำระยะยาวในรอบถัดไป ทำ "หลังตอบผู้ใช้แล้ว" เป็นงานเบื้องหลัง
	// เดิมรอขั้นนี้ให้เสร็จก่อนค่อยตอบ ทำให้ผู้ใช้รอเพิ่มอีก 1 รอบเรียก Gemini โดยไม่จำเป็น
	// (ถ้าขั้นนี้พลาด แค่คำตอบนี้ไม่ถูกใช้เป็นความทรงจำเก่า แชทยังทำงานปกติ)
	aiChatID := aiChat.ChatID
	aiReplyText := aiReply
	go func() {
		emb, embErr := h.Gemini.EmbedText(aiReplyText)
		if embErr != nil {
			log.Printf("Embedding error (คำตอบ AI): %v", embErr)
			return
		}
		if _, err := h.DB.Exec(`UPDATE chats SET embedding = $1 WHERE chat_id = $2`, pq.Array(emb), aiChatID); err != nil {
			log.Printf("บันทึก embedding คำตอบ AI ไม่สำเร็จ: %v", err)
		}
	}()

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
