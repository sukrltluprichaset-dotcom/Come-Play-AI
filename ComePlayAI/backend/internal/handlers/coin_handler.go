package handlers

import (
	"database/sql"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type CoinHandler struct {
	DB *sql.DB
}

func NewCoinHandler(db *sql.DB) *CoinHandler {
	return &CoinHandler{DB: db}
}

func (h *CoinHandler) GetBalance(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var coin models.Coin
	err := h.DB.QueryRow(
		`SELECT coin_id, balance, user_id, updated_at FROM coins WHERE user_id = $1`,
		userID,
	).Scan(&coin.CoinID, &coin.Balance, &coin.UserID, &coin.UpdatedAt)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดข้อมูลกระเป๋าเหรียญไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusOK, coin)
}
