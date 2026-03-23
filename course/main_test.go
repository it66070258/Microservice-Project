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

// ---- Global Test Variables ----

var (
	testReadConn  *pgx.Conn
	testWriteConn *pgx.Conn
	testDBConns   *DBConnections
)

// ---- Test Setup & Teardown ----

func TestMain(m *testing.M) {
	setupTestDB()
	gin.SetMode(gin.TestMode)
	fmt.Println("Connected to test database (read and write)")
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

	testReadConn, err = pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}

	testWriteConn, err = pgx.Connect(context.Background(), connStr)
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
		testReadConn.Close(context.Background())
	}
	if testWriteConn != nil {
		testWriteConn.Close(context.Background())
	}
}

// ---- Database Helpers ----

func resetDB() {
	ctx := context.Background()

	// Ensure schemas exist
	ensureSchemas()

	// Truncate and Seed
	if _, err := testWriteConn.Exec(ctx, `TRUNCATE TABLE course RESTART IDENTITY CASCADE`); err != nil {
		log.Fatal("Failed to truncate:", err)
	}

	seedData := `
		INSERT INTO course ("course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "current_student", "prerequisite") VALUES
		(1, 'Mathematics',      3, ARRAY['1', '2'], 'Monday',    '09:00:00', '12:00:00', 30, 'open', ARRAY['3'],   NULL),
		(2, 'Physics',          3, ARRAY['1', '3'], 'Tuesday',   '13:00:00', '16:00:00', 30, 'open', ARRAY['1'],   ARRAY['Mathematics']),
		(3, 'Computer Science', 3, ARRAY['1'],      'Wednesday', '09:00:00', '13:00:00', 80, 'open', ARRAY['2'],   NULL)
	`
	if _, err := testWriteConn.Exec(ctx, seedData); err != nil {
		log.Fatal("Failed to seed:", err)
	}
}

func ensureSchemas() {
	ctx := context.Background()

	courseSchema := `
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
		);`

	if _, err := testWriteConn.Exec(ctx, courseSchema); err != nil {
		log.Fatal("Failed to ensure process schema:", err)
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

func TestGetCourses_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	w := performRequest(router, "GET", "/courses", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var courses []Course
	err := json.Unmarshal(w.Body.Bytes(), &courses)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(courses))
}

func TestGetCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	w := performRequest(router, "GET", "/courses/1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var course Course
	err := json.Unmarshal(w.Body.Bytes(), &course)
	assert.Nil(t, err)
	assert.Equal(t, 1, course.CourseID)
	assert.Equal(t, "Mathematics", course.Subject)
}

func TestGetCourse_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	w := performRequest(router, "GET", "/courses/999", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

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

	w := performRequest(router, "POST", "/courses", body)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Course created successfully", resp["message"])

	var count int
	testWriteConn.QueryRow(context.Background(), `SELECT COUNT(*) FROM course`).Scan(&count)
	assert.Equal(t, 4, count)
}

func TestCreateCourse_BadRequest(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	body := map[string]interface{}{"subject": "Incomplete"}

	w := performRequest(router, "POST", "/courses", body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	body := map[string]interface{}{"state": "closed"}

	w := performRequest(router, "PUT", "/courses/1", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Course updated successfully", resp["message"])

	var state string
	testWriteConn.QueryRow(context.Background(), `SELECT "state" FROM course WHERE course_id = 1`).Scan(&state)
	assert.Equal(t, "closed", state)
}

func TestUpdateCourse_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	body := map[string]interface{}{"state": "closed"}

	w := performRequest(router, "PUT", "/courses/999", body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDeleteCourse_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := performRequest(router, "DELETE", "/courses/1", nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var count int
	testWriteConn.QueryRow(context.Background(), `SELECT COUNT(*) FROM course`).Scan(&count)
	assert.Equal(t, 2, count)
}

func TestDeleteCourse_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	w := performRequest(router, "DELETE", "/courses/999", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
