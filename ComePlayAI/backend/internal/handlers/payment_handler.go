package handlers

import (
	"database/sql"

	"github.com/gofiber/fiber/v2"

	"comeplayai-backend/internal/models"
)

type PaymentHandler struct {
	DB *sql.DB
}

func NewPaymentHandler(db *sql.DB) *PaymentHandler {
	return &PaymentHandler{DB: db}
}

// ----- ดูแพ็กเกจที่มีขาย -----

func (h *PaymentHandler) ListPackages(c *fiber.Ctx) error {
	rows, err := h.DB.Query(`SELECT package_id, name, price, coin_amount FROM packages ORDER BY price ASC`)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดรายการแพ็กเกจไม่สำเร็จ")
	}
	defer rows.Close()

	packages := []models.Package{}
	for rows.Next() {
		var p models.Package
		if err := rows.Scan(&p.PackageID, &p.Name, &p.Price, &p.CoinAmount); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดรายการแพ็กเกจไม่สำเร็จ")
		}
		packages = append(packages, p)
	}

	return writeJSON(c, fiber.StatusOK, packages)
}

// ----- ยืนยันชำระเงิน (จำลอง) + เติมเหรียญทันที -----

type createPaymentRequest struct {
	PackageID int64  `json:"package_id"`
	Method    string `json:"method"` // "PromptPay" หรือ "Credit Card"
}

func (h *PaymentHandler) CreatePayment(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	var req createPaymentRequest
	if err := c.BodyParser(&req); err != nil {
		return writeError(c, fiber.StatusBadRequest, "รูปแบบข้อมูลไม่ถูกต้อง")
	}
	if req.Method != "PromptPay" && req.Method != "Credit Card" {
		return writeError(c, fiber.StatusBadRequest, "ช่องทางการชำระเงินไม่ถูกต้อง")
	}

	var pkgName string
	var price float64
	var coinAmount int
	err := h.DB.QueryRow(
		`SELECT name, price, coin_amount FROM packages WHERE package_id = $1`,
		req.PackageID,
	).Scan(&pkgName, &price, &coinAmount)
	if err == sql.ErrNoRows {
		return writeError(c, fiber.StatusNotFound, "ไม่พบแพ็กเกจนี้")
	} else if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เกิดข้อผิดพลาดในระบบ")
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
		return writeError(c, fiber.StatusInternalServerError, "บันทึกรายการชำระเงินไม่สำเร็จ")
	}

	var newBalance int
	err = tx.QueryRow(
		`UPDATE coins SET balance = balance + $1 WHERE user_id = $2 RETURNING balance`,
		coinAmount, userID,
	).Scan(&newBalance)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "เติมเหรียญไม่สำเร็จ")
	}

	if err := tx.Commit(); err != nil {
		return writeError(c, fiber.StatusInternalServerError, "ทำรายการไม่สำเร็จ")
	}

	return writeJSON(c, fiber.StatusCreated, fiber.Map{
		"payment":     payment,
		"new_balance": newBalance,
	})
}

// ----- ดูประวัติการทำรายการของตัวเอง -----

func (h *PaymentHandler) ListMyPayments(c *fiber.Ctx) error {
	userID := userIDFromContext(c)

	rows, err := h.DB.Query(
		`SELECT payment_id, user_id, package_id, payment_method, amount, status, package_name, coin_amount, payment_time
		 FROM payments WHERE user_id = $1 ORDER BY payment_time DESC`,
		userID,
	)
	if err != nil {
		return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติทำรายการไม่สำเร็จ")
	}
	defer rows.Close()

	payments := []models.Payment{}
	for rows.Next() {
		var p models.Payment
		if err := rows.Scan(&p.PaymentID, &p.UserID, &p.PackageID, &p.PaymentMethod, &p.Amount, &p.Status, &p.PackageName, &p.CoinAmount, &p.PaymentTime); err != nil {
			return writeError(c, fiber.StatusInternalServerError, "โหลดประวัติทำรายการไม่สำเร็จ")
		}
		payments = append(payments, p)
	}

	return writeJSON(c, fiber.StatusOK, payments)
}
