package handlers

import (
	"database/sql"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/lib/pq"

	"comeplayai-backend/internal/models"
)

type PlaylistHandler struct {
	DB *sql.DB
}

func NewPlaylistHandler(db *sql.DB) *PlaylistHandler {
	return &PlaylistHandler{DB: db}
}

type playlistRequest struct {
	Name string `json:"name"`
}

// ----- Create: สร้างเพลย์ลิสต์ใหม่ (ต่อท้ายลำดับสุดท้ายของผู้ใช้คนนั้นเสมอ) -----
func (h *PlaylistHandler) Create(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req playlistRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 50 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อเพลย์ลิสต์ต้องไม่ว่างและไม่เกิน 50 ตัวอักษร")
	}

	var playlist models.UserPlaylist
	err := h.DB.QueryRow(
		`INSERT INTO user_playlists (user_id, name, display_order)
		 VALUES ($1, $2, COALESCE((SELECT MAX(display_order) + 1 FROM user_playlists WHERE user_id = $1), 0))
		 RETURNING playlist_id, user_id, name, display_order, created_at`,
		userID, req.Name,
	).Scan(&playlist.PlaylistID, &playlist.UserID, &playlist.Name, &playlist.DisplayOrder, &playlist.CreatedAt)
	if err != nil {
		log.Printf("[Playlist.Create] insert user_playlists ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "สร้างเพลย์ลิสต์ไม่สำเร็จ")
	}
	playlist.Characters = []models.Character{}

	return writeJSON(c, fiber.StatusCreated, playlist)
}

// ----- List: เพลย์ลิสต์ทั้งหมดของผู้ใช้ พร้อมตัวละครข้างในแต่ละอัน เรียงตามลำดับที่ตั้งไว้ -----
// โหลดตัวละครของทุกเพลย์ลิสต์ในคำสั่งเดียว (query ที่สองใช้ ANY($1) กับรายชื่อ playlist_id ทั้งหมด)
// แทนที่จะวน query ทีละเพลย์ลิสต์ (N+1) เพื่อกันเปลืองการเชื่อมต่อฐานข้อมูลตอนผู้ใช้มีหลายเพลย์ลิสต์
func (h *PlaylistHandler) List(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	playlistRows, err := h.DB.Query(
		`SELECT playlist_id, user_id, name, display_order, created_at
		 FROM user_playlists WHERE user_id = $1 ORDER BY display_order ASC, playlist_id ASC`,
		userID,
	)
	if err != nil {
		log.Printf("[Playlist.List] query user_playlists ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "โหลดเพลย์ลิสต์ไม่สำเร็จ")
	}
	defer playlistRows.Close()

	playlists := []models.UserPlaylist{}
	playlistIDs := []int64{}
	indexByID := map[int64]int{}
	for playlistRows.Next() {
		var p models.UserPlaylist
		if err := playlistRows.Scan(&p.PlaylistID, &p.UserID, &p.Name, &p.DisplayOrder, &p.CreatedAt); err != nil {
			log.Printf("[Playlist.List] scan user_playlists ล้มเหลว: %v", err)
			return writeError(c, fiber.StatusInternalServerError, "โหลดเพลย์ลิสต์ไม่สำเร็จ")
		}
		p.Characters = []models.Character{}
		indexByID[p.PlaylistID] = len(playlists)
		playlists = append(playlists, p)
		playlistIDs = append(playlistIDs, p.PlaylistID)
	}

	if len(playlists) == 0 {
		return writeJSON(c, fiber.StatusOK, playlists)
	}

	// หมายเหตุ: ไม่ใช้ characterColumns (ไม่ระบุ alias ตาราง) ตรงนี้ตรงๆ เพราะ query นี้ join สองตาราง
	// (user_playlist_characters upc + characters ch) ซึ่งมีคอลัมน์ชื่อซ้ำกัน (เช่น character_id, created_at)
	// ทำให้ Postgres แยกไม่ออกว่าหมายถึงคอลัมน์ของตารางไหน (ambiguous) จึงต้องระบุ ch. นำหน้าทุกคอลัมน์ของ characters เอง
	itemRows, err := h.DB.Query(
		`SELECT upc.playlist_id, ch.character_id, ch.name, ch.personality, ch.avatar_url,
		        ch.usage_count, ch.rating, ch.review_count, ch.is_shared,
		        ch.user_id, ch.created_at, ch.updated_at
		 FROM user_playlist_characters upc
		 JOIN characters ch ON ch.character_id = upc.character_id
		 WHERE upc.playlist_id = ANY($1)
		 ORDER BY upc.playlist_id ASC, upc.display_order ASC`,
		pq.Array(playlistIDs),
	)
	if err != nil {
		log.Printf("[Playlist.List] query user_playlist_characters ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "โหลดเพลย์ลิสต์ไม่สำเร็จ")
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var playlistID int64
		var avatar sql.NullString
		var ch models.Character
		if err := itemRows.Scan(
			&playlistID,
			&ch.CharacterID, &ch.Name, &ch.Personality, &avatar,
			&ch.UsageCount, &ch.Rating, &ch.ReviewCount, &ch.IsShared,
			&ch.UserID, &ch.CreatedAt, &ch.UpdatedAt,
		); err != nil {
			log.Printf("[Playlist.List] scan user_playlist_characters ล้มเหลว: %v", err)
			return writeError(c, fiber.StatusInternalServerError, "โหลดเพลย์ลิสต์ไม่สำเร็จ")
		}
		if avatar.Valid {
			ch.AvatarURL = &avatar.String
		}
		if idx, ok := indexByID[playlistID]; ok {
			playlists[idx].Characters = append(playlists[idx].Characters, ch)
		}
	}

	return writeJSON(c, fiber.StatusOK, playlists)
}

// ----- Rename: แก้ไขชื่อเพลย์ลิสต์ (เฉพาะเจ้าของ) -----
func (h *PlaylistHandler) Rename(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสเพลย์ลิสต์ไม่ถูกต้อง")
	}

	var req playlistRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 50 {
		return writeError(c, fiber.StatusBadRequest, "ชื่อเพลย์ลิสต์ต้องไม่ว่างและไม่เกิน 50 ตัวอักษร")
	}

	result, err := h.DB.Exec(`UPDATE user_playlists SET name = $1 WHERE playlist_id = $2 AND user_id = $3`, req.Name, id, userID)
	if err != nil {
		log.Printf("[Playlist.Rename] update user_playlists ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "แก้ไขชื่อเพลย์ลิสต์ไม่สำเร็จ")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบเพลย์ลิสต์นี้ หรือคุณไม่ใช่เจ้าของ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "แก้ไขชื่อเพลย์ลิสต์สำเร็จ"})
}

// ----- Delete: ลบเพลย์ลิสต์ (ตัวละครในเพลย์ลิสต์จะถูกลบตามไปด้วยผ่าน FK ON DELETE CASCADE) -----
func (h *PlaylistHandler) Delete(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสเพลย์ลิสต์ไม่ถูกต้อง")
	}

	result, err := h.DB.Exec(`DELETE FROM user_playlists WHERE playlist_id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		log.Printf("[Playlist.Delete] delete user_playlists ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "ลบเพลย์ลิสต์ไม่สำเร็จ")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบเพลย์ลิสต์นี้ หรือคุณไม่ใช่เจ้าของ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบเพลย์ลิสต์สำเร็จ"})
}

type reorderPlaylistsRequest struct {
	PlaylistIDs []int64 `json:"playlist_ids"`
}

// ----- Reorder: จัดลำดับเพลย์ลิสต์ใหม่ทั้งหมดตามลำดับที่ส่งมา (index ในลิสต์ = display_order ใหม่) -----
func (h *PlaylistHandler) Reorder(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req reorderPlaylistsRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}

	tx, err := h.DB.Begin()
	if err != nil {
		log.Printf("[Playlist.Reorder] เริ่ม transaction ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	defer tx.Rollback()

	// ใช้ WHERE ...AND user_id = $3 กันไม่ให้ผู้ใช้ส่ง playlist_id ของคนอื่นมาสลับลำดับได้ (แค่ไม่ match แถวไหนเลย เงียบๆ)
	for i, playlistID := range req.PlaylistIDs {
		if _, err := tx.Exec(
			`UPDATE user_playlists SET display_order = $1 WHERE playlist_id = $2 AND user_id = $3`,
			i, playlistID, userID,
		); err != nil {
			log.Printf("[Playlist.Reorder] update display_order ล้มเหลว (playlist_id=%d): %v", playlistID, err)
			return writeError(c, fiber.StatusInternalServerError, "จัดลำดับเพลย์ลิสต์ไม่สำเร็จ")
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[Playlist.Reorder] commit transaction ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "จัดลำดับเพลย์ลิสต์ไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "จัดลำดับเพลย์ลิสต์สำเร็จ"})
}

// ----- AddCharacter: เพิ่มตัวละครเข้าเพลย์ลิสต์ (ต้องเป็นเจ้าของเพลย์ลิสต์ และตัวละครต้องมีอยู่จริง) -----
// กดซ้ำได้โดยไม่ error (ON CONFLICT DO NOTHING) เหมือนกับ favorites
func (h *PlaylistHandler) AddCharacter(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	playlistID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสเพลย์ลิสต์ไม่ถูกต้อง")
	}
	characterID, err := strconv.ParseInt(c.Params("characterId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	var ownsPlaylist bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM user_playlists WHERE playlist_id = $1 AND user_id = $2)`, playlistID, userID).Scan(&ownsPlaylist); err != nil {
		log.Printf("[Playlist.AddCharacter] ตรวจสอบเจ้าของเพลย์ลิสต์ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !ownsPlaylist {
		return writeError(c, fiber.StatusNotFound, "ไม่พบเพลย์ลิสต์นี้ หรือคุณไม่ใช่เจ้าของ")
	}

	var characterExists bool
	if err := h.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM characters WHERE character_id = $1)`, characterID).Scan(&characterExists); err != nil {
		log.Printf("[Playlist.AddCharacter] ตรวจสอบตัวละครล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}
	if !characterExists {
		return writeError(c, fiber.StatusNotFound, "ไม่พบตัวละครนี้")
	}

	if _, err := h.DB.Exec(
		`INSERT INTO user_playlist_characters (playlist_id, character_id, display_order)
		 VALUES ($1, $2, COALESCE((SELECT MAX(display_order) + 1 FROM user_playlist_characters WHERE playlist_id = $1), 0))
		 ON CONFLICT (playlist_id, character_id) DO NOTHING`,
		playlistID, characterID,
	); err != nil {
		log.Printf("[Playlist.AddCharacter] insert user_playlist_characters ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "เพิ่มตัวละครเข้าเพลย์ลิสต์ไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "เพิ่มตัวละครเข้าเพลย์ลิสต์แล้ว"})
}

// ----- RemoveCharacter: ลบตัวละครออกจากเพลย์ลิสต์ -----
func (h *PlaylistHandler) RemoveCharacter(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	playlistID, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสเพลย์ลิสต์ไม่ถูกต้อง")
	}
	characterID, err := strconv.ParseInt(c.Params("characterId"), 10, 64)
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "รหัสตัวละครไม่ถูกต้อง")
	}

	result, err := h.DB.Exec(
		`DELETE FROM user_playlist_characters
		 WHERE playlist_id = $1 AND character_id = $2
		   AND EXISTS (SELECT 1 FROM user_playlists WHERE playlist_id = $1 AND user_id = $3)`,
		playlistID, characterID, userID,
	)
	if err != nil {
		log.Printf("[Playlist.RemoveCharacter] delete user_playlist_characters ล้มเหลว: %v", err)
		return writeError(c, fiber.StatusInternalServerError, "ลบตัวละครออกจากเพลย์ลิสต์ไม่สำเร็จ")
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return writeError(c, fiber.StatusNotFound, "ไม่พบรายการนี้ในเพลย์ลิสต์ หรือคุณไม่ใช่เจ้าของเพลย์ลิสต์")
	}

	return writeJSON(c, fiber.StatusOK, fiber.Map{"message": "ลบตัวละครออกจากเพลย์ลิสต์แล้ว"})
}
