-- คำนวณ rating (คะแนนเฉลี่ย) และ review_count (จำนวนรีวิว) ของตัวละครทุกตัวใหม่
-- จากข้อมูลจริงในตาราง character_reviews (ใช้ครั้งเดียว เพื่อแก้ข้อมูลเก่าที่ค้างเป็น 0
-- เพราะโค้ดเวอร์ชันก่อนหน้านี้ไม่เคยอัปเดตค่านี้กลับมาที่ตาราง characters เลย)
--
-- ปลอดภัย รันซ้ำได้หลายรอบไม่มีผลเสีย (idempotent) ไม่ลบข้อมูลอะไร แค่คำนวณค่าใหม่ทับ

BEGIN;

UPDATE characters c
SET
    rating = COALESCE(
        (SELECT AVG(cr.rating) FROM character_reviews cr WHERE cr.character_id = c.character_id),
        0
    ),
    review_count = (
        SELECT COUNT(*) FROM character_reviews cr WHERE cr.character_id = c.character_id
    );

-- เช็คผลก่อน commit จริง: ดูตัวละครที่มีรีวิวอยู่ ว่าคะแนนคำนวณออกมาถูกต้องมั้ย
SELECT character_id, name, rating, review_count
FROM characters
WHERE review_count > 0
ORDER BY review_count DESC;

COMMIT;
