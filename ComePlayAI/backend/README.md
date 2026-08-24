# Come Play AI — Backend (Go)

โครงสร้างไฟล์:

```
backend/
  cmd/server/main.go          -> จุดเริ่มรันเซิร์ฟเวอร์ + endpoint /health
  internal/config/config.go   -> โหลดค่าจาก .env
  internal/database/database.go -> เชื่อมต่อ PostgreSQL
  go.mod / go.sum
  .env.example
```

ใช้ไลบรารี `github.com/lib/pq` เป็นตัวเชื่อมต่อ PostgreSQL (pure Go, ไม่มี
dependency ภายนอกซับซ้อน) และ `github.com/joho/godotenv` สำหรับอ่านไฟล์ `.env`

## วิธีรัน (ทดสอบแล้วว่าใช้ได้จริงกับ PostgreSQL ที่ติดตั้งไว้)

1. เปิด Terminal ที่โฟลเดอร์ `backend/`

2. คัดลอกไฟล์ env:
   ```
   copy .env.example .env
   ```
   (Mac/Linux ใช้ `cp .env.example .env`)

3. เปิดไฟล์ `.env` แล้วใส่ **รหัสผ่าน PostgreSQL ที่ตั้งไว้ตอนติดตั้ง**
   ลงในบรรทัด `DB_PASSWORD=`

4. โหลด dependency ให้ครบ:
   ```
   go mod tidy
   ```

5. รันเซิร์ฟเวอร์:
   ```
   go run ./cmd/server
   ```

6. ควรเห็นข้อความ:
   ```
   เชื่อมต่อฐานข้อมูล "comeplayai" สำเร็จ
   เซิร์ฟเวอร์เริ่มทำงานที่ http://localhost:8080
   ```

7. เปิดเบราว์เซอร์ไปที่ **http://localhost:8080/health**
   ถ้าทุกอย่างถูกต้อง จะเห็น:
   ```json
   {"status":"ok","database_status":"connected","package_count":3}
   ```
   ตัวเลข `package_count: 3` มาจากข้อมูลจริงในตาราง `packages` ที่ seed.sql
   ใส่ไว้ — ถ้าเห็นเลข 3 แปลว่า Go เชื่อมกับฐานข้อมูลที่ทำไว้ได้จริง

## ถ้าเจอ error ตอนรัน

- **`DB_PASSWORD is not set`** — ลืมใส่รหัสผ่านในไฟล์ `.env`
- **`ping ไม่ผ่าน` / `password authentication failed`** — รหัสผ่านใน `.env`
  ไม่ตรงกับรหัสที่ตั้งไว้ตอนติดตั้ง PostgreSQL หรือ `DB_USER` ไม่ตรง (ถ้าใช้
  PostgreSQL ที่ติดตั้งลงเครื่องตรงๆ ปกติ user คือ `postgres`)
- **`connection refused`** — PostgreSQL service ไม่ได้รันอยู่ (เช็คใน
  Windows: Services > postgresql-x64-16 ต้องเป็น Running)
- **`database "comeplayai" does not exist`** — ยังไม่ได้สร้างฐานข้อมูลใน
  Navicat หรือชื่อฐานข้อมูลใน `.env` สะกดผิด

## ขั้นต่อไป (ยังไม่ได้ทำ)

`/health` เป็นแค่จุดเริ่มพิสูจน์ว่าต่อฐานข้อมูลติด ขั้นต่อไปที่ควรทำคือ
API จริงตามขอบเขตในเล่ม เช่น:
- `POST /auth/register`, `POST /auth/login`
- `GET/POST/PUT/DELETE /characters`
- `POST /chats` (พร้อมต่อ LLM API ภายหลัง)
- `GET /characters/:id/reviews`, `POST /characters/:id/reviews`
- ระบบยืนยันตัวตนด้วย JWT

บอกได้เลยครับว่าอยากให้เริ่มทำอันไหนก่อน
