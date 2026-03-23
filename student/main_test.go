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
	if _, err := testWriteConn.Exec(ctx, `TRUNCATE TABLE student RESTART IDENTITY CASCADE`); err != nil {
		log.Fatal("Failed to truncate:", err)
	}

	// We create a hash of 'password123' for testing login
	hashedPassword, _ := hashPassword("password123")

	seedData := fmt.Sprintf(`
		INSERT INTO student ("student_id", "first_name", "last_name", "email", "password", "birthdate", "gender", "year_level", "graded_subject") VALUES
		(1, 'John', 'Doe', 'john@example.com', '%s', '2000-01-01', 'Male', 2, ARRAY['Computer Science']),
		(2, 'Jane', 'Smith', 'jane@example.com', '%s', '2001-02-02', 'Female', 1, ARRAY['Mathematics'])
	`, hashedPassword, hashedPassword)

	if _, err := testWriteConn.Exec(ctx, seedData); err != nil {
		log.Fatal("Failed to seed:", err)
	}
}

func ensureSchemas() {
	ctx := context.Background()

	studentSchema := `
		DROP TABLE IF EXISTS student CASCADE;
		CREATE TABLE IF NOT EXISTS student (
			"student_id" INTEGER NOT NULL UNIQUE,
			"first_name" VARCHAR(255) NOT NULL,
			"last_name" VARCHAR(255) NOT NULL,
			"email" VARCHAR(255) NOT NULL,
			"password" VARCHAR(255) NOT NULL,
			"birthdate" VARCHAR(255) NOT NULL,
			"gender" VARCHAR(255) NOT NULL,
			"year_level" INTEGER NOT NULL,
			"graded_subject" VARCHAR(255) ARRAY,
			PRIMARY KEY("student_id")
		);`

	if _, err := testWriteConn.Exec(ctx, studentSchema); err != nil {
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

func TestGetStudents_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	w := performRequest(router, "GET", "/students", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var students []Student
	err := json.Unmarshal(w.Body.Bytes(), &students)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(students))
}

func TestGetStudent_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	w := performRequest(router, "GET", "/students/1", nil)

	assert.Equal(t, http.StatusOK, w.Code)

	var student Student
	err := json.Unmarshal(w.Body.Bytes(), &student)
	assert.Nil(t, err)
	assert.Equal(t, 1, student.StudentID)
	assert.Equal(t, "John", student.FirstName)
}

func TestGetStudent_NotFound(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)
	w := performRequest(router, "GET", "/students/999", nil)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRegisterStudent_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{
		"student_id":     3,
		"first_name":     "Alice",
		"last_name":      "Wonderland",
		"email":          "alice@example.com",
		"password":       "alice123",
		"birthdate":      "2002-03-03",
		"gender":         "Female",
		"year_level":     1,
		"graded_subject": []string{},
	}

	w := performRequest(router, "POST", "/register", body)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "ลงทะเบียนสำเร็จ", resp["message"])

	var count int
	testWriteConn.QueryRow(context.Background(), `SELECT COUNT(*) FROM student`).Scan(&count)
	assert.Equal(t, 3, count)
}

func TestLoginStudent_Success(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{
		"email":    "john@example.com",
		"password": "password123",
	}

	w := performRequest(router, "POST", "/login", body)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "Login สำเร็จ", resp["message"])
	assert.Equal(t, float64(1), resp["student_id"])
}

func TestLoginStudent_InvalidCredentials(t *testing.T) {
	resetDB()
	router := SetupRouter(testDBConns)

	body := map[string]interface{}{
		"email":    "john@example.com",
		"password": "wrongpassword",
	}

	w := performRequest(router, "POST", "/login", body)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
