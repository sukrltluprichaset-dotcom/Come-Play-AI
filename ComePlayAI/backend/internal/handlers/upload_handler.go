package handlers

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const maxUploadSize = 20 << 20 // 20 MB

type UploadHandler struct {
	SupabaseURL        string
	SupabaseServiceKey string
	SupabaseBucket     string
	HTTPClient         *http.Client
}

func NewUploadHandler(supabaseURL, serviceKey, bucket string) *UploadHandler {
	return &UploadHandler{
		SupabaseURL:        supabaseURL,
		SupabaseServiceKey: serviceKey,
		SupabaseBucket:     bucket,
		HTTPClient:         &http.Client{Timeout: 30 * time.Second},
	}
}

func (h *UploadHandler) Upload(c *fiber.Ctx) error {
	header, err := c.FormFile("file")
	if err != nil {
		return writeError(c, fiber.StatusBadRequest, "ไม่พบไฟล์ที่ส่งมา")
	}

	if header.Size > maxUploadSize {
		return writeError(c, fiber.StatusBadRequest, "ไฟล์มีขนาดใหญ่เกินไป (สูงสุด 20MB)")
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	allowedExt := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
		".mp4": true, ".webm": true,
	}
	if !allowedExt[ext] {
		return writeError(c, fiber.StatusBadRequest, "รองรับเฉพาะไฟล์ภาพ (jpg, png, gif, webp) หรือวิดีโอ (mp4, webm)")
	}

	file, err := header.Open()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อ่านไฟล์ไม่สำเร็จ")
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "อ่านไฟล์ไม่สำเร็จ")
	}

	filename := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)

	log.Printf("กำลังอัปโหลดไปที่ Supabase: URL=%s, Bucket=%s, KeyLength=%d", h.SupabaseURL, h.SupabaseBucket, len(h.SupabaseServiceKey))

	// อัปโหลดไปยัง Supabase Storage ผ่าน REST API โดยตรง
	uploadURL := fmt.Sprintf("%s/storage/v1/object/%s/%s", h.SupabaseURL, h.SupabaseBucket, filename)

	req, err := http.NewRequest(http.MethodPost, uploadURL, bytes.NewReader(fileBytes))
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "สร้าง request ไม่สำเร็จ")
	}
	req.Header.Set("apikey", h.SupabaseServiceKey)
	req.Header.Set("Authorization", "Bearer "+h.SupabaseServiceKey)
	req.Header.Set("Content-Type", header.Header.Get("Content-Type"))

	resp, err := h.HTTPClient.Do(req)
	if err != nil {
		log.Printf("Supabase Storage เชื่อมต่อไม่สำเร็จ: %v (URL ที่ใช้: %s)", err, uploadURL)
		return writeError(c, fiber.StatusInternalServerError, "อัปโหลดไฟล์ไม่สำเร็จ")
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("Supabase Storage ตอบกลับ error (status %d): %s", resp.StatusCode, string(respBody))
		return writeError(c, fiber.StatusInternalServerError, fmt.Sprintf("อัปโหลดไฟล์ไม่สำเร็จ (status %d): %s", resp.StatusCode, string(respBody)))
	}

	publicURL := fmt.Sprintf("%s/storage/v1/object/public/%s/%s", h.SupabaseURL, h.SupabaseBucket, filename)

	return writeJSON(c, fiber.StatusOK, fiber.Map{"url": publicURL})
}
