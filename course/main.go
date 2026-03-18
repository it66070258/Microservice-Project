package main

// dependency
import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/sony/gobreaker"
)

// DBConnections เก็บ connections สำหรับ read และ write
type DBConnections struct {
	ReadConn  *pgx.Conn
	WriteConn *pgx.Conn
}

// connectToReadDB เชื่อม database สำหรับ read (replica)
func connectToReadDB() *pgx.Conn {
	host := os.Getenv("DB_READ_HOST")
	if host == "" {
		host = "localhost" // fallback to localhost
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL READ database!")

	err = conn.Ping(context.Background())
	if err != nil {
		log.Fatal("READ database ping failed:", err)
	}

	return conn
}

// connectToWriteDB เชื่อม database สำหรับ write (master)
func connectToWriteDB() *pgx.Conn {
	host := os.Getenv("DB_WRITE_HOST")
	if host == "" {
		host = "localhost" // fallback to localhost
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL WRITE database!")

	err = conn.Ping(context.Background())
	if err != nil {
		log.Fatal("WRITE database ping failed:", err)
	}

	return conn
}

// สร้างประเภทตัวแปร
type Course struct {
	CourseID       int       `json:"course_id"`
	Subject        string    `json:"subject"`
	Credit         int       `json:"credit"`
	Section        []string  `json:"section"`
	DayOfWeek      string    `json:"day_of_week"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Capacity       int       `json:"capacity"`
	State          string    `json:"state"`
	CurrentStudent []string  `json:"current_student"`
	Prerequisite   []string  `json:"prerequisite"`
}

func SetupRouter(dbConns *DBConnections) *gin.Engine {
	r := gin.Default()

	// สร้าง circuit breaker สำหรับ database read
	readSettings := gobreaker.Settings{
		Name:        "Database-Read-Operations",
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
		Name:        "Database-Write-Operations",
		MaxRequests: 3,
		Interval:    time.Minute,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.2
		},
	}
	writeCircuitBreaker := gobreaker.NewCircuitBreaker(writeSettings)

	// สร้าง metrics logger (ใช้ write connection)
	metricsLogger := NewMetricsLogger(dbConns.WriteConn)

	// เพิ่ม metrics middleware (ใช้ read circuit breaker เป็น default)
	r.Use(MetricsMiddleware(metricsLogger, readCircuitBreaker))

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

	// Metrics Endpoints
	// GET /metrics/circuit-breaker - ตรวจสอบสถานะ circuit breaker
	r.GET("/metrics/circuit-breaker", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"read_circuit_breaker":  readCircuitBreaker.State().String(),
			"write_circuit_breaker": writeCircuitBreaker.State().String(),
		})
	})

	// GET /metrics/recent - ดึง metrics ล่าสุด
	r.GET("/metrics/recent", func(c *gin.Context) {
		metrics, err := metricsLogger.GetRecentMetrics(100)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch recent metrics: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, metrics)
	})

	// GET /metrics/aggregate - ดึงข้อมูลสถิติรวม
	r.GET("/metrics/aggregate", func(c *gin.Context) {
		hoursParam := c.DefaultQuery("hours", "1")
		hours := 1
		if h, err := time.ParseDuration(hoursParam + "h"); err == nil {
			hours = int(h.Hours())
		}

		startTime := time.Now().Add(-time.Duration(hours) * time.Hour)
		endTime := time.Now()

		metrics, err := metricsLogger.GetAggregateMetrics(startTime, endTime)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch aggregate metrics: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, metrics)
	})

	// GET /metrics/endpoint/:endpoint - ดึงสถิติของ endpoint เฉพาะ
	r.GET("/metrics/endpoint/:endpoint", func(c *gin.Context) {
		endpoint := c.Param("endpoint")
		hoursParam := c.DefaultQuery("hours", "1")
		hours := 1
		if h, err := time.ParseDuration(hoursParam + "h"); err == nil {
			hours = int(h.Hours())
		}

		stats, err := metricsLogger.GetEndpointStats(endpoint, hours)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch endpoint stats: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, stats)
	})

	return r
}

func main() {
	// เชื่อมต่อ read และ write databases
	readConn := connectToReadDB()
	writeConn := connectToWriteDB()
	defer readConn.Close(context.Background())
	defer writeConn.Close(context.Background())

	dbConns := &DBConnections{
		ReadConn:  readConn,
		WriteConn: writeConn,
	}

	r := SetupRouter(dbConns)
	r.Run(":8000") // รันที่ localhost:8000

	fmt.Println("Course Service started on port 8000") // เช็ค
}
