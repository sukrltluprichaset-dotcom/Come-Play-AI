package handlers

import (
	"database/sql"
	"net/http"

	"comeplayai-backend/internal/models"
)

type CoinHandler struct {
	DB *sql.DB
}

func NewCoinHandler(db *sql.DB) *CoinHandler {
	return &CoinHandler{DB: db}
}

func (h *CoinHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	var coin models.Coin
	err := h.DB.QueryRow(
		`SELECT coin_id, balance, user_id, updated_at FROM coins WHERE user_id = $1`,
		userID,
	).Scan(&coin.CoinID, &coin.Balance, &coin.UserID, &coin.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดข้อมูลกระเป๋าเหรียญไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusOK, coin)
}
