# ระบบแจ้งเตือน — วิธีติดตั้ง (ทำตามลำดับ)

ไฟล์ใหม่ในชุดนี้ (คัดลอกไปวางในโปรเจกต์ ไม่ทับไฟล์เดิม):

| ไฟล์ในชุดนี้ | วางที่ |
|---|---|
| `sql/001_create_notifications.sql` | รันใน Supabase SQL Editor / Navicat (รันซ้ำได้) |
| `backend/internal/models/notification.go` | `ComePlayAI/backend/internal/models/` |
| `backend/internal/handlers/notification_handler.go` | `ComePlayAI/backend/internal/handlers/` |
| `frontend/notifications_snippet.js` | วางต่อท้าย `<script>` ใน `index.html` |

ส่วนที่ต้อง "แก้ไฟล์เดิม" มี 6 จุด (ข้อ 2–7 ด้านล่าง) แต่ละจุดสั้นๆ

---

## 1) ฐานข้อมูล
รัน `sql/001_create_notifications.sql` กับฐานข้อมูลจริง (Supabase) ก่อนรีสตาร์ท backend

## 2) `backend/cmd/server/main.go` — เพิ่ม route
วางต่อจากกลุ่ม route อื่นๆ (เช่น หลังบล็อก coinHandler) **ทุกเส้นต้องมี `authMW`**

```go
	notificationHandler := handlers.NewNotificationHandler(db)
	app.Get("/api/notifications", authMW, notificationHandler.List)
	app.Get("/api/notifications/unread-count", authMW, notificationHandler.UnreadCount)
	app.Put("/api/notifications/read-all", authMW, notificationHandler.MarkAllRead)
	app.Put("/api/notifications/:id/read", authMW, notificationHandler.MarkRead)
```

## 3) จุดที่ "สร้างแจ้งเตือน" — รีวิวใหม่ (`handlers/review_handler.go`)
1. เพิ่ม `"fmt"` ใน import
2. ใน `CreateOrUpdate` วางต่อจากบรรทัด `_ = h.DB.QueryRow(`SELECT username ...`).Scan(&review.Username)` (ก่อน `return writeJSON(...)`):

```go
	// แจ้งเตือนเจ้าของตัวละคร: เฉพาะ "รีวิวใหม่" (ถ้าเป็นการแก้รีวิวเดิม created_at ≠ updated_at จะไม่แจ้งซ้ำ)
	// และไม่แจ้งเตือนตัวเอง เรียกหลัง Commit สำเร็จแล้วเท่านั้น (แจ้งเตือนพลาดต้องไม่ทำให้รีวิวล้ม)
	if review.CreatedAt.Equal(review.UpdatedAt) {
		var ownerID int64
		var charName string
		if err := h.DB.QueryRow(`SELECT user_id, name FROM characters WHERE character_id = $1`, characterID).Scan(&ownerID, &charName); err == nil && ownerID != userID {
			_ = CreateNotification(h.DB, ownerID, "review",
				fmt.Sprintf("%s รีวิวตัวละคร \"%s\" ของคุณ %d ดาว", review.Username, charName, review.Rating), &characterID)
		}
	}
```

## 4) จุดที่ "สร้างแจ้งเตือน" — ปิดเคสรายงาน (`handlers/admin_handler.go`)
ใน `UpdateReportStatus` วางต่อจากบล็อก `if affected == 0 { ... }` (ก่อน `return writeJSON(...)`):

```go
	// แจ้งผู้ส่งรายงานว่าเคสถูกดำเนินการแล้ว
	var reporterID, reportedCharID int64
	if err := h.DB.QueryRow(`SELECT user_id, character_id FROM reports WHERE report_id = $1`, reportID).Scan(&reporterID, &reportedCharID); err == nil {
		msg := "รายงานของคุณได้รับการดำเนินการแล้ว"
		if req.Status == "rejected" {
			msg = "รายงานของคุณถูกตรวจสอบแล้ว และไม่พบการกระทำผิด"
		}
		_ = CreateNotification(h.DB, reporterID, "report", msg, &reportedCharID)
	}
```

## 5) จุดที่ "สร้างแจ้งเตือน" — ชวนเพื่อนสำเร็จ (`handlers/auth_handler.go`, ฟังก์ชัน `Register`)
1. ประกาศตัวแปรก่อนบล็อก `if req.ReferralCode != "" {` :
```go
	var rewardedReferrerID int64
```
2. ในบล็อก `if _, err := tx.Exec(INSERT INTO referrals ...); err == nil {` ที่บรรทัดแรกใส่:
```go
				rewardedReferrerID = referrerID
```
3. หลัง `tx.Commit()` สำเร็จ (ก่อนสร้าง token) ใส่:
```go
	if rewardedReferrerID != 0 {
		_ = CreateNotification(h.DB, rewardedReferrerID, "referral",
			fmt.Sprintf("%s สมัครสมาชิกด้วยโค้ดชวนเพื่อนของคุณ คุณได้รับ %d เหรียญ", user.Username, referralRewardToReferrer), nil)
	}
```

## 6) `index.html` — เปิด/ปิดกระดิ่ง
1. วางโค้ดจาก `frontend/notifications_snippet.js` ต่อท้ายใน `<script>`
2. ใน `enterApp(user)` เพิ่มบรรทัดสุดท้าย (หลัง `refreshDailyRewardBanner();`):
```js
            initNotifications();
```
3. ใน `handleLogout()` ภายใน callback ของ `showCustomConfirm` เพิ่มต่อจาก `localStorage.removeItem('user');`:
```js
                stopNotifications();
```
กระดิ่งอยู่มุมขวาบนของจอ (position fixed) ถ้าไปทับปุ่มอื่น ปรับ `top-4 right-4` ในฟังก์ชัน `initNotifications()` ได้

## 7) รีสตาร์ท backend แล้ว push/deploy (Render) แล้วทดสอบ

### ทดสอบด้วยมือ (ใช้เป็นหลักฐานใน Black Box ได้)
1. ล็อกอินบัญชี A สร้างตัวละครแชร์สาธารณะ
2. ล็อกอินบัญชี B (อีกเบราว์เซอร์) รีวิวตัวละครของ A → ภายใน ~45 วินาที กระดิ่งของ A ขึ้นเลขแดง 1
3. A กดกระดิ่ง → เห็นข้อความ "B รีวิวตัวละคร ... ของคุณ N ดาว" → กดข้อความ → เลขแดงหาย
4. B แก้รีวิวเดิม → A ไม่ได้รับแจ้งเตือนซ้ำ
5. A รีวิวตัวละครของตัวเอง → ไม่มีแจ้งเตือน
6. กด "อ่านทั้งหมด" → เลขแดงหาย
7. ไม่ล็อกอินแล้วเปิด `GET /api/notifications` → 401

### สิ่งที่ผมทดสอบให้แล้ว
- `gofmt` ผ่าน, ตรวจชนิดข้อมูลของ Go ผ่านโดยใช้ stub ของ Fiber (ยังคอมไพล์เต็มโปรเจกต์ไม่ได้ในเครื่องผม เพราะ go.mod ต้องการโมดูลเวอร์ชันใหม่ที่โหลดไม่ได้แบบออฟไลน์ — ให้คุณรัน `go build ./...` ในเครื่องอีกครั้ง)
- SQL: สร้างตารางซ้ำได้, ตัดของเก่าเหลือ 100 รายการต่อผู้ใช้, `UPDATE ... AND user_id` ทำให้แก้แจ้งเตือนคนอื่นไม่ได้, ลบตัวละคร → แจ้งเตือนอยู่ต่อ (character_id เป็น NULL), ลบผู้ใช้ → แจ้งเตือนหายตาม
- ไวยากรณ์ JavaScript ผ่าน (`node --check`) แต่ยังไม่ได้ลองคลิกในเบราว์เซอร์จริง

---

## เอกสารที่ต้องอัปเดตตาม

**Data Dictionary — ตาราง Notifications**

| ชื่อระเบียน | รายละเอียด | ชนิดและขนาด | ข้อบังคับ | ตัวอย่าง |
|---|---|---|---|---|
| notification_id | รหัสการแจ้งเตือน | BIGINT | Primary Key, Auto Increment | 7001 |
| user_id | ผู้รับการแจ้งเตือน | BIGINT | Foreign Key → users(user_id) | 1001 |
| type | ชนิดการแจ้งเตือน | VARCHAR(30) | Not Null | review |
| message | ข้อความแจ้งเตือน | TEXT | Not Null | มีผู้รีวิวตัวละครของคุณ 5 ดาว |
| character_id | ตัวละครที่เกี่ยวข้อง | BIGINT | Foreign Key → characters, Null ได้ | 2001 |
| is_read | อ่านแล้วหรือยัง | BOOLEAN | Not Null, Default false | false |
| created_at | วันเวลาที่แจ้ง | TIMESTAMPTZ | Not Null, Default Now() | 2026-10-05 10:00:00 |

**ER:** เพิ่ม `users 1───* notifications` (และเส้นอ้อม `characters 1───* notifications`, ไม่บังคับ)

**DFD:** เพิ่มกระบวนการ "จัดการการแจ้งเตือน" + data store D10 แฟ้มการแจ้งเตือน
- ข้อมูลเข้า: เหตุการณ์จากกระบวนการรีวิว / รายงาน / ชวนเพื่อน (ไปบันทึกลง D10); คำขอดูรายการ/อ่านแล้วจากผู้ใช้งาน
- ข้อมูลออก: รายการแจ้งเตือน + จำนวนที่ยังไม่อ่านกลับไปยังผู้ใช้งาน

**Process Description:** ID ใหม่ เช่น 8.4 "ดูและอ่านการแจ้งเตือน" — ตรวจ token → ดึงรายการของผู้ใช้จาก D10 → แสดงตัวเลขที่ยังไม่อ่าน → ผู้ใช้กดอ่าน → อัปเดต `is_read`

**Black Box (ตารางใหม่ ≥ 3 กรณี):** ดูรายการแจ้งเตือน / กดอ่านทีละรายการ / อ่านทั้งหมด / ไม่ล็อกอินเรียกดูไม่ได้ / รีวิวตัวเองไม่แจ้งเตือน

**บทที่ 1 ขอบเขต 1.3.1:** เพิ่มข้อ "ดูการแจ้งเตือนได้" และเพิ่มภาพหน้ากระดิ่งใน 3.2.5
