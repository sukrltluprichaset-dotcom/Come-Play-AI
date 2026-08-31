package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"comeplayai-backend/internal/models"
)

type PaymentHandler struct {
	DB *sql.DB
}

func NewPaymentHandler(db *sql.DB) *PaymentHandler {
	return &PaymentHandler{DB: db}
}

// ----- ดูแพ็กเกจที่มีขาย -----

func (h *PaymentHandler) ListPackages(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(`SELECT package_id, name, price, coin_amount FROM packages ORDER BY price ASC`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดรายการแพ็กเกจไม่สำเร็จ")
		return
	}
	defer rows.Close()

	packages := []models.Package{}
	for rows.Next() {
		var p models.Package
		if err := rows.Scan(&p.PackageID, &p.Name, &p.Price, &p.CoinAmount); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดรายการแพ็กเกจไม่สำเร็จ")
			return
		}
		packages = append(packages, p)
	}

	writeJSON(w, http.StatusOK, packages)
}

// ----- ยืนยันชำระเงิน (จำลอง) + เติมเหรียญทันที -----

type createPaymentRequest struct {
	PackageID int64  `json:"package_id"`
	Method    string `json:"method"` // "PromptPay" หรือ "Credit Card"
}

func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	var req createPaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
		return
	}
	if req.Method != "PromptPay" && req.Method != "Credit Card" {
		writeError(w, http.StatusBadRequest, "ช่องทางการชำระเงินไม่ถูกต้อง")
		return
	}

	var pkgName string
	var price float64
	var coinAmount int
	err := h.DB.QueryRow(
		`SELECT name, price, coin_amount FROM packages WHERE package_id = $1`,
		req.PackageID,
	).Scan(&pkgName, &price, &coinAmount)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "ไม่พบแพ็กเกจนี้")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}

	tx, err := h.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
		return
	}
	defer tx.Rollback()

	var payment models.Payment
	err = tx.QueryRow(
		`INSERT INTO payments (user_id, package_id, payment_method, amount, status, package_name, coin_amount, payment_time)
		 VALUES ($1, $2, $3, $4, 'success', $5, $6, NOW())
		 RETURNING payment_id, user_id, package_id, payment_method, amount, status, package_name, coin_amount, payment_time`,
		userID, req.PackageID, req.Method, price, pkgName, coinAmount,
	).Scan(&payment.PaymentID, &payment.UserID, &payment.PackageID, &payment.PaymentMethod, &payment.Amount, &payment.Status, &payment.PackageName, &payment.CoinAmount, &payment.PaymentTime)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "บันทึกรายการชำระเงินไม่สำเร็จ")
		return
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 RETURNING balance`,
		coinAmount, userID,
	).Scan(&newBalance)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "เติมเหรียญไม่สำเร็จ")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "ทำรายการไม่สำเร็จ")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"payment":     payment,
		"new_balance": newBalance,
	})
}

// ----- ดูประวัติการทำรายการของตัวเอง -----

func (h *PaymentHandler) ListMyPayments(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)

	rows, err := h.DB.Query(
		`SELECT payment_id, user_id, package_id, payment_method, amount, status, package_name, coin_amount, payment_time
		 FROM payments WHERE user_id = $1 ORDER BY payment_time DESC`,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "โหลดประวัติทำรายการไม่สำเร็จ")
		return
	}
	defer rows.Close()

	payments := []models.Payment{}
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.PaymentID, &p.UserID, &p.PackageID, &p.PaymentMethod, &p.Amount, &p.Status, &p.PackageName, &p.CoinAmount, &p.PaymentTime); err != nil {
			writeError(w, http.StatusInternalServerError, "โหลดประวัติทำรายการไม่สำเร็จ")
			return
		}
		payments = append(payments, p)
	}

	writeJSON(w, http.StatusOK, payments)
}
