package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
)

// ---- DB Connection ----

var testReadConn *pgx.Conn
var testWriteConn *pgx.Conn
var testDBConns *DBConnections

// SetupTestRouter สร้าง router สำหรับการทดสอบ (ไม่ใช้ metrics middleware)
func SetupTestRouter(dbConns *DBConnections) *gin.Engine {
	r := gin.Default()

	// สร้าง circuit breaker สำหรับ database read
	readSettings := gobreaker.Settings{
		Name:        "Test-Database-Read-Operations",
		MaxRequests: 3,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.2
		},
	}
	readCircuitBreaker := gobreaker.NewCircuitBreaker(readSettings)

	// สร้าง circuit breaker สำหรับ database write
	writeSettings := gobreaker.Settings{
		Name:        "Test-Database-Write-Operations",
		MaxRequests: 3,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.2
		},
	}
	writeCircuitBreaker := gobreaker.NewCircuitBreaker(writeSettings)

	// ไม่ใช้ metrics middleware ในการ test เพื่อหลีกเลี่ยงปัญหา conn busy

	// ดึง course ออกมาทั้งหมด (READ)
	r.GET("/courses", func(c *gin.Context) {
		var courses []Course

		_, err := readCircuitBreaker.Execute(func() (interface{}, error) {
			rows, err := dbConns.ReadConn.Query(context.Background(), `SELECT "course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "current_student", "prerequisite" FROM course`)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			for rows.Next() {
				var course Course
				err := rows.Scan(
					&course.CourseID,
					&course.Subject,
					&course.Credit,
					&course.Section,
					&course.DayOfWeek,
					&course.StartTime,
					&course.EndTime,
					&course.Capacity,
					&course.State,
					&course.CurrentStudent,
					&course.Prerequisite,
				)
				if err != nil {
					return nil, err
				}
				courses = append(courses, course)
			}
			return courses, nil
		})

		if err == gobreaker.ErrOpenState {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable (circuit breaker is open)"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query courses: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, courses)
	})

	// ดึง course ตัวเดียว (READ)
	r.GET("/courses/:id", func(c *gin.Context) {
		id := c.Param("id")
		var course Course

		_, err := readCircuitBreaker.Execute(func() (interface{}, error) {
			return nil, dbConns.ReadConn.QueryRow(context.Background(),
				`SELECT "course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "current_student", "prerequisite" FROM course WHERE "course_id" = $1`,
				id,
			).Scan(
				&course.CourseID,
				&course.Subject,
				&course.Credit,
				&course.Section,
				&course.DayOfWeek,
				&course.StartTime,
				&course.EndTime,
				&course.Capacity,
				&course.State,
				&course.CurrentStudent,
				&course.Prerequisite,
			)
		})

		if err == gobreaker.ErrOpenState {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable (circuit breaker is open)"})
			return
		}
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Course not found"})
			return
		}

		c.JSON(http.StatusOK, course)
	})

	// อัพเดท course (WRITE)
	r.PUT("/courses/:id", func(c *gin.Context) {
		id := c.Param("id")

		var body struct {
			Subject        *string  `json:"subject"`
			Credit         *int     `json:"credit"`
			Section        []string `json:"section"`
			DayOfWeek      *string  `json:"day_of_week"`
			StartTime      *string  `json:"start_time"`
			EndTime        *string  `json:"end_time"`
			Capacity       *int     `json:"capacity"`
			State          *string  `json:"state"`
			CurrentStudent []string `json:"current_student"`
			Prerequisite   []string `json:"prerequisite"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body: " + err.Error()})
			return
		}

		_, err := writeCircuitBreaker.Execute(func() (interface{}, error) {
			result, err := dbConns.WriteConn.Exec(context.Background(),
				`UPDATE course SET
					"subject"         = COALESCE($1, "subject"),
					"credit"          = COALESCE($2, "credit"),
					"section"         = COALESCE($3, "section"),
					"day_of_week"     = COALESCE($4, "day_of_week"),
					"start_time"      = COALESCE($5::TIME, "start_time"),
					"end_time"        = COALESCE($6::TIME, "end_time"),
					"capacity"        = COALESCE($7, "capacity"),
					"state"           = COALESCE($8, "state"),
					"current_student" = COALESCE($9, "current_student"),
					"prerequisite"    = COALESCE($10, "prerequisite")
				WHERE course_id = $11`,
				body.Subject,
				body.Credit,
				body.Section,
				body.DayOfWeek,
				body.StartTime,
				body.EndTime,
				body.Capacity,
				body.State,
				body.CurrentStudent,
				body.Prerequisite,
				id,
			)
			if err != nil {
				return nil, err
			}
			if result.RowsAffected() == 0 {
				return nil, fmt.Errorf("course not found")
			}
			return result, nil
		})

		if err == gobreaker.ErrOpenState {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable (circuit breaker is open)"})
			return
		}
		if err != nil {
			if err.Error() == "course not found" {
				c.JSON(http.StatusNotFound, gin.H{"error": "Course not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update course: " + err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Course updated successfully"})
	})

	// เพิ่มข้อมูล course (WRITE)
	r.POST("/courses", func(c *gin.Context) {
		var body struct {
			CourseID       int      `json:"course_id"      binding:"required"`
			Subject        string   `json:"subject"        binding:"required"`
			Credit         int      `json:"credit"         binding:"required"`
			Section        []string `json:"section"        binding:"required"`
			DayOfWeek      string   `json:"day_of_week"    binding:"required"`
			StartTime      string   `json:"start_time"     binding:"required"`
			EndTime        string   `json:"end_time"       binding:"required"`
			Capacity       int      `json:"capacity"       binding:"required"`
			State          string   `json:"state"          binding:"required"`
			CurrentStudent []string `json:"current_student"`
			Prerequisite   []string `json:"prerequisite"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON body: " + err.Error()})
			return
		}

		_, err := writeCircuitBreaker.Execute(func() (interface{}, error) {
			return dbConns.WriteConn.Exec(context.Background(),
				`INSERT INTO course ("course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "current_student", "prerequisite")
				VALUES ($1, $2, $3, $4, $5, $6::TIME, $7::TIME, $8, $9, $10, $11)`,
				body.CourseID,
				body.Subject,
				body.Credit,
				body.Section,
				body.DayOfWeek,
				body.StartTime,
				body.EndTime,
				body.Capacity,
				body.State,
				body.CurrentStudent,
				body.Prerequisite,
			)
		})

		if err == gobreaker.ErrOpenState {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable (circuit breaker is open)"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create course: " + err.Error()})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"message": "Course created successfully"})
	})

	// ลบข้อมูล course (WRITE)
	r.DELETE("/courses/:id", func(c *gin.Context) {
		id := c.Param("id")

		_, err := writeCircuitBreaker.Execute(func() (interface{}, error) {
			result, err := dbConns.WriteConn.Exec(context.Background(),
				`DELETE FROM course WHERE course_id = $1`,
				id,
			)
			if err != nil {
				return nil, err
			}
			if result.RowsAffected() == 0 {
				return nil, fmt.Errorf("course not found")
			}
			return result, nil
		})

		if err == gobreaker.ErrOpenState {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Service temporarily unavailable (circuit breaker is open)"})
			return
		}
		if err != nil {
			if err.Error() == "course not found" {
				c.JSON(http.StatusNotFound, gin.H{"error": "Course not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete course: " + err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Course deleted successfully"})
	})

	return r
}

func TestMain(m *testing.M) {
	connStr := "user=postgres password=1234 host=localhost port=5432 dbname=register sslmode=disable"
	var err error

	// เชื่อมต่อ read connection
	testReadConn, err = pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}

	// เชื่อมต่อ write connection (ในการทดสอบใช้ database เดียวกัน)
	testWriteConn, err = pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}

	// สร้าง DBConnections สำหรับ test
	testDBConns = &DBConnections{
		ReadConn:  testReadConn,
		WriteConn: testWriteConn,
	}

	gin.SetMode(gin.TestMode)
	fmt.Println("Connected to test database (read and write)")

	// รัน tests
	exitCode := m.Run()

	// Cleanup: ปิด connections
	testReadConn.Close(context.Background())
	testWriteConn.Close(context.Background())

	os.Exit(exitCode)
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
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/courses/999", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST /courses - สร้างสำเร็จ
func TestCreateCourse_Success(t *testing.T) {
	resetDB()
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

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
	router := SetupTestRouter(testDBConns)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/courses/999", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
