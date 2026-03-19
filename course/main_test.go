package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

// ---- DB Connection ----

var testReadConn *pgx.Conn
var testWriteConn *pgx.Conn
var testDBConns *DBConnections

func TestMain(m *testing.M) {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	var err error

	// เชื่อมต่อ read connection
	testReadConn, err = pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}
	defer testReadConn.Close(context.Background())

	// เชื่อมต่อ write connection (ในการทดสอบใช้ database เดียวกัน)
	testWriteConn, err = pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}
	defer testWriteConn.Close(context.Background())

	// สร้าง DBConnections สำหรับ test
	testDBConns = &DBConnections{
		ReadConn:  testReadConn,
		WriteConn: testWriteConn,
	}

	gin.SetMode(gin.TestMode)
	fmt.Println("Connected to test database (read and write)")
	m.Run()
}

// ---- Reset DB ----

func resetDB() {
	// Ensure schema exists so tests can run against a clean database.
	// ใช้ write connection สำหรับการสร้างตารางและเพิ่มข้อมูล
	_, err := testWriteConn.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS course (
			"course_id" INTEGER NOT NULL UNIQUE,
			"subject" VARCHAR(255) NOT NULL,
			"credit" INTEGER NOT NULL,
			"section" VARCHAR(255) ARRAY NOT NULL,
			"day_of_week" VARCHAR(255) NOT NULL,
			"start_time" TIME NOT NULL,
			"end_time" TIME NOT NULL,
			"capacity" INTEGER NOT NULL,
			"state" VARCHAR(255) NOT NULL,
			"current_student" VARCHAR(255) ARRAY,
			"prerequisite" VARCHAR(255) ARRAY,
			PRIMARY KEY("course_id")
		);
	`)
	if err != nil {
		log.Fatal("Failed to ensure schema:", err)
	}

	// สร้างตาราง metrics ถ้ายังไม่มี
	_, err = testWriteConn.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS request_metrics (
			id SERIAL PRIMARY KEY,
			timestamp TIMESTAMP NOT NULL DEFAULT NOW(),
			endpoint VARCHAR(255) NOT NULL,
			method VARCHAR(10) NOT NULL,
			status_code INTEGER NOT NULL,
			response_time_ms FLOAT NOT NULL,
			circuit_breaker_state VARCHAR(20),
			error_message TEXT,
			created_at TIMESTAMP DEFAULT NOW()
		);
	`)
	if err != nil {
		log.Fatal("Failed to ensure metrics schema:", err)
	}

	_, err = testWriteConn.Exec(context.Background(), `TRUNCATE TABLE course RESTART IDENTITY CASCADE`)
	if err != nil {
		log.Fatal("Failed to truncate:", err)
	}
	_, err = testWriteConn.Exec(context.Background(), `
		INSERT INTO course ("course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "current_student", "prerequisite") VALUES
		(1, 'Mathematics',      3, ARRAY['1', '2'], 'Monday',    '09:00:00', '12:00:00', 30, 'open', ARRAY['3'],   NULL),
		(2, 'Physics',          3, ARRAY['1', '3'], 'Tuesday',   '13:00:00', '16:00:00', 30, 'open', ARRAY['1'],   ARRAY['Mathematics']),
		(3, 'Computer Science', 3, ARRAY['1'],      'Wednesday', '09:00:00', '13:00:00', 80, 'open', ARRAY['2'],   NULL)
	`)
	if err != nil {
		log.Fatal("Failed to seed:", err)
	}
}

// ---- Tests ----

// GET /courses
func TestGetCourses_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/courses", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var courses []Course
	err := json.Unmarshal(w.Body.Bytes(), &courses)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(courses))
}

// GET /courses/:id - พบ
func TestGetCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/courses/1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var course Course
	err := json.Unmarshal(w.Body.Bytes(), &course)
	assert.Nil(t, err)
	assert.Equal(t, 1, course.CourseID)
	assert.Equal(t, "Mathematics", course.Subject)
	assert.Equal(t, 3, course.Credit)
	assert.Equal(t, 30, course.Capacity)
}

// GET /courses/:id - ไม่พบ
func TestGetCourse_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/courses/999", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST /courses - สร้างสำเร็จ
func TestCreateCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{
		"course_id":       4,
		"subject":         "Chemistry",
		"credit":          3,
		"section":         []string{"1"},
		"day_of_week":     "Friday",
		"start_time":      "09:00:00",
		"end_time":        "12:00:00",
		"capacity":        20,
		"state":           "open",
		"current_student": []string{},
		"prerequisite":    []string{},
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/courses", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Course created successfully", resp["message"])

	var count int
	testWriteConn.QueryRow(context.Background(), `SELECT COUNT(*) FROM course`).Scan(&count)
	assert.Equal(t, 4, count)
}

// POST /courses - ข้อมูลไม่ครบ
func TestCreateCourse_BadRequest(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{"subject": "Incomplete"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/courses", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// PUT /courses/:id - อัพเดทสำเร็จ
func TestUpdateCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{"state": "closed"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/courses/1", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Course updated successfully", resp["message"])

	var state string
	testWriteConn.QueryRow(context.Background(), `SELECT "state" FROM course WHERE course_id = 1`).Scan(&state)
	assert.Equal(t, "closed", state)
}

// PUT /courses/:id - ไม่พบ
func TestUpdateCourse_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{"state": "closed"}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/courses/999", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// DELETE /courses/:id - ลบสำเร็จ
func TestDeleteCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/courses/1", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Course deleted successfully", resp["message"])

	var count int
	testWriteConn.QueryRow(context.Background(), `SELECT COUNT(*) FROM course`).Scan(&count)
	assert.Equal(t, 2, count)
}

// DELETE /courses/:id - ไม่พบ
func TestDeleteCourse_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/courses/999", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
