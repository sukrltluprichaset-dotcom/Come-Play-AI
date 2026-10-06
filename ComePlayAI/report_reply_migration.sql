-- เพิ่มระบบตอบกลับรายงานจากแอดมิน (ไม่ต้องสร้างตารางใหม่ แค่เพิ่ม 2 คอลัมน์ในตาราง reports)
-- รันได้หลายครั้งโดยไม่พัง (IF NOT EXISTS)
ALTER TABLE reports ADD COLUMN IF NOT EXISTS admin_reply TEXT;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS replied_at  TIMESTAMPTZ;
