package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

// ---- Global Test Variables ----

var (
	testReadConn  *sql.DB
	testWriteConn *sql.DB
	testDBConns   *DBConnections
)

// ---- Test Setup & Teardown ----

func TestMain(m *testing.M) {
	setupTestDB()
	gin.SetMode(gin.TestMode)
	fmt.Println("Connected to test database")
	code := m.Run()
	teardownTestDB()
	os.Exit(code)
}

func setupTestDB() {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	var err error

	testReadConn, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}

	testWriteConn, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}

	testDBConns = &DBConnections{
		ReadConn:  testReadConn,
		WriteConn: testWriteConn,
	}
}

func teardownTestDB() {
	if testReadConn != nil {
		testReadConn.Close()
	}
	if testWriteConn != nil {
		testWriteConn.Close()
	}
}

// ---- Database Helpers ----

func resetDB() {
	ensureSchemas()

	if _, err := testWriteConn.Exec(`TRUNCATE TABLE student, course, enrollment RESTART IDENTITY CASCADE`); err != nil {
		log.Fatal("Failed to truncate tables:", err)
	}

	seedData := `
		INSERT INTO student (student_id, graded_subject) VALUES
		(1, ARRAY['Mathematics']),
		(2, ARRAY[]::VARCHAR[]);

		INSERT INTO course (course_id, subject, credit, capacity, section, current_student, prerequisite, day_of_week, start_time, end_time, state) VALUES
		(1, 'Math 1', 3, 30, ARRAY['1'], ARRAY[]::VARCHAR[], NULL, 'Monday', '09:00:00', '12:00:00', 'open'),
		(2, 'Physics', 3, 30, ARRAY['1'], ARRAY[]::VARCHAR[], ARRAY['Mathematics'], 'Tuesday', '13:00:00', '16:00:00', 'open'),
		(3, 'Com Sci', 3, 1, ARRAY['1'], ARRAY['3'], NULL, 'Wednesday', '09:00:00', '12:00:00', 'closed'),
		(4, 'Biology', 3, 30, ARRAY['1'], ARRAY[]::VARCHAR[], NULL, 'Monday', '10:00:00', '13:00:00', 'open');
	`
	if _, err := testWriteConn.Exec(seedData); err != nil {
		log.Fatal("Failed to seed data:", err)
	}
}

func ensureSchemas() {
	schema := `
		CREATE TABLE IF NOT EXISTS student (
			student_id INTEGER PRIMARY KEY,
			graded_subject VARCHAR(255) ARRAY
		);
		CREATE TABLE IF NOT EXISTS course (
			course_id INTEGER PRIMARY KEY,
			subject VARCHAR(255),
			credit INTEGER,
			capacity INTEGER,
			section VARCHAR(255) ARRAY,
			current_student VARCHAR(255) ARRAY,
			prerequisite VARCHAR(255) ARRAY,
			day_of_week VARCHAR(20),
			start_time TIME,
			end_time TIME,
			state VARCHAR(20)
		);
		CREATE TABLE IF NOT EXISTS enrollment (
			id SERIAL PRIMARY KEY,
			student_id INTEGER,
			course_id INTEGER[]
		);
	`
	if _, err := testWriteConn.Exec(schema); err != nil {
		log.Fatal("Failed to setup schema:", err)
	}
}

// ---- HTTP Helpers ----

func performRequest(router *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(jsonBody)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req, _ := http.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ---- Tests ----

// 1. ทดสอบ Request Body ไม่ถูกต้อง
func TestEnroll_InvalidRequest(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns, nil)

	body := map[string]interface{}{"student_id": 1} // ขาด course_ids
	w := performRequest(router, "POST", "/enroll", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// 2. ทดสอบลงทะเบียนวิชาที่ไม่มีในระบบ
func TestEnroll_CourseNotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns, nil)

	body := map[string]interface{}{"student_id": 1, "course_ids": []int{999}}
	w := performRequest(router, "POST", "/enroll", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// 3. ทดสอบลงทะเบียนวิชาที่ยังไม่ผ่านวิชาบังคับ (Prerequisite)
func TestEnroll_MissingPrerequisite(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns, nil)

	// นักเรียน 2 ยังไม่ผ่านวิชา Mathematics จึงไม่สามารถลงวิชา 2 (Physics) ได้
	body := map[string]interface{}{"student_id": 2, "course_ids": []int{2}}
	w := performRequest(router, "POST", "/enroll", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// 4. ทดสอบลงทะเบียนวิชาที่ปิดหรือเต็มแล้ว (Closed)
func TestEnroll_CourseClosed(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns, nil)

	// วิชา 3 ถูกตั้ง state เป็น closed และเต็ม
	body := map[string]interface{}{"student_id": 1, "course_ids": []int{3}}
	w := performRequest(router, "POST", "/enroll", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// 5. ทดสอบลงทะเบียนวิชาที่เวลาเรียนชนกันเองใน Request ใหม่
func TestEnroll_TimeOverlapInNewCourses(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns, nil)

	// วิชา 1 (09:00 - 12:00) ชนกับ วิชา 4 (10:00 - 13:00) ในวันจันทร์เหมือนกัน
	body := map[string]interface{}{"student_id": 1, "course_ids": []int{1, 4}}
	w := performRequest(router, "POST", "/enroll", body)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
