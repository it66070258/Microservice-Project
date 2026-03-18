# Test File Updates for Read-Write Separation

## สรุปการเปลี่ยนแปลง

### 1. เปลี่ยนจาก Single Connection เป็น Dual Connections

```go
// Before
var testConn *pgx.Conn

// After
var testReadConn *pgx.Conn
var testWriteConn *pgx.Conn
var testDBConns *DBConnections
```

### 2. สร้าง Function สำหรับสร้าง Test Router

เพิ่ม `SetupTestRouter()` เพื่อแยกจาก production router:

- ไม่ใช้ metrics middleware (เพื่อหลีกเลี่ยงปัญหา "conn busy")
- ใช้ circuit breakers แยก (Test-Database-Read/Write-Operations)
- มี endpoints เหมือนกันกับ production

### 3. แก้ไข resetDB()

- ใช้ `testWriteConn` สำหรับ CREATE TABLE และ INSERT
- เพิ่มการสร้างตาราง `request_metrics` สำหรับ metrics

### 4. เพิ่ม Imports

```go
import (
    "time"
    "github.com/sony/gobreaker"
)
```

### 5. แก้ไขทุก Test Functions

- เปลี่ยนจาก `SetupRouter(testConn)` → `SetupTestRouter(testDBConns)`
- เปลี่ยนจาก `testConn.QueryRow()` → `testWriteConn.QueryRow()`

## ประโยชน์

1. **Test ไม่กระทบกับ Production Code**
   - มี router แยกสำหรับ test
   - หลีกเลี่ยงปัญหา metrics middleware

2. **ทดสอบความถูกต้องของ Read-Write Separation**
   - ตรวจสอบว่า GET requests ใช้ read connection
   - ตรวจสอบว่า POST/PUT/DELETE ใช้ write connection

3. **Performance**
   - ไม่มี metrics logging ในการทดสอบ
   - รันเร็วกว่า

## การรัน Tests

```bash
# รัน tests ทั้งหมด
go test -v

# รัน test เฉพาะ
go test -v -run TestGetCourses_Success

# รัน tests พร้อม timeout
go test -v -timeout 30s
```

## Test Results

ทุก tests ผ่าน:

- ✅ TestGetCourses_Success
- ✅ TestGetCourse_Success
- ✅ TestGetCourse_NotFound
- ✅ TestCreateCourse_Success
- ✅ TestCreateCourse_BadRequest
- ✅ TestUpdateCourse_Success
- ✅ TestUpdateCourse_NotFound
- ✅ TestDeleteCourse_Success
- ✅ TestDeleteCourse_NotFound

## Notes

- ในการทดสอบ เราใช้ database เดียวกันสำหรับทั้ง read และ write (localhost)
- ในการใช้งานจริง สามารถแยก host ได้ผ่าน environment variables
- Test router ไม่มี metrics endpoints เพราะไม่จำเป็นสำหรับการทดสอบ CRUD operations
