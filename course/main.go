package main

// dependency
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sony/gobreaker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func registerConsul(serviceName string, port int) {
	go func() {
		time.Sleep(5 * time.Second)
		reg := map[string]interface{}{
			"ID":      serviceName,
			"Name":    serviceName,
			"Address": serviceName,
			"Port":    port,
			"Check": map[string]interface{}{
				"HTTP":     fmt.Sprintf("http://%s:%d/metrics", serviceName, port),
				"Interval": "10s",
				"Timeout":  "5s",
			},
		}

		payload, _ := json.Marshal(reg)
		req, _ := http.NewRequest("PUT", "http://consul:8500/v1/agent/service/register", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Println("Consul Registration Failed:", err)
			return
		}
		defer resp.Body.Close()
		log.Println("Consul Registration Success for", serviceName)
	}()
}

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)
	httpDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "Duration of HTTP requests in seconds",
		},
		[]string{"method", "path"},
	)
)

func init() {
	prometheus.MustRegister(httpRequestsTotal)
	prometheus.MustRegister(httpDuration)
}

func PrometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start).Seconds()

		status := fmt.Sprintf("%d", c.Writer.Status())
		method := c.Request.Method
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		httpRequestsTotal.WithLabelValues(method, path, status).Inc()
		httpDuration.WithLabelValues(method, path).Observe(duration)
	}
}

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

// connectToRabbitMQ เชื่อมต่อ RabbitMQ
func connectToRabbitMQ() (*amqp.Connection, *amqp.Channel) {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
	}

	var rabbitConn *amqp.Connection
	var err error
	for i := 0; i < 5; i++ {
		rabbitConn, err = amqp.Dial(rabbitURL)
		if err == nil {
			break
		}
		log.Printf("RabbitMQ not ready, retrying in 5s... (%d/5)", i+1)
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		log.Fatal("RabbitMQ Connection Error:", err)
	}

	rabbitChannel, err := rabbitConn.Channel()
	if err != nil {
		log.Fatal("RabbitMQ Channel Error:", err)
	}

	// Declare request queue
	_, err = rabbitChannel.QueueDeclare("course_enrollment_request", true, false, false, false, nil)
	if err != nil {
		log.Fatal("Queue Declaration Error:", err)
	}

	log.Println("Successfully connected to RabbitMQ (Course Service)")
	return rabbitConn, rabbitChannel
}

// EnrollmentMessage ข้อมูลที่รับจาก enrollment service
type EnrollmentMessage struct {
	StudentID int   `json:"student_id"`
	CourseIDs []int `json:"course_ids"`
}

// EnrollmentResponse ข้อมูลตอบกลับไปยัง enrollment service
type EnrollmentResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
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

	// ใช้งาน Prometheus Middleware
	r.Use(PrometheusMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

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

// startEnrollmentConsumer รับข้อความจาก enrollment service
func startEnrollmentConsumer(dbConn *pgx.Conn, rabbitChannel *amqp.Channel) {
	msgs, err := rabbitChannel.Consume(
		"course_enrollment_request", // queue
		"",                          // consumer tag
		false,                       // auto-ack
		false,                       // exclusive
		false,                       // no-local
		false,                       // no-wait
		nil,                         // args
	)
	if err != nil {
		log.Fatal("Failed to register consumer:", err)
	}

	log.Println("Course Consumer: Waiting for enrollment requests...")

	for d := range msgs {
		var msg EnrollmentMessage
		err := json.Unmarshal(d.Body, &msg)
		if err != nil {
			log.Printf("Error unmarshaling message: %v", err)
			d.Nack(false, false)
			continue
		}

		log.Printf("Received enrollment request: StudentID=%d, CourseIDs=%v", msg.StudentID, msg.CourseIDs)

		// ประมวลผลการลงทะเบียน
		response := processEnrollment(dbConn, msg)

		// ส่ง response กลับ
		responseBody, _ := json.Marshal(response)
		err = rabbitChannel.PublishWithContext(context.Background(),
			"",        // exchange
			d.ReplyTo, // routing key (reply queue)
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType:   "application/json",
				CorrelationId: d.CorrelationId,
				Body:          responseBody,
			})

		if err != nil {
			log.Printf("Failed to send response: %v", err)
			d.Nack(false, true)
		} else {
			log.Printf("Sent response: Success=%v, Message=%s", response.Success, response.Message)
			d.Ack(false)
		}
	}
}

// processEnrollment ประมวลผลการลงทะเบียน
func processEnrollment(dbConn *pgx.Conn, msg EnrollmentMessage) EnrollmentResponse {
	ctx := context.Background()

	// เริ่ม transaction
	tx, err := dbConn.Begin(ctx)
	if err != nil {
		return EnrollmentResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to start transaction: %v", err),
		}
	}
	defer tx.Rollback(ctx)

	// ตรวจสอบและอัพเดทแต่ละ course
	for _, courseID := range msg.CourseIDs {
		var capacity int
		var currentStudents []string
		var state string

		// ดึงข้อมูล course พร้อม lock เพื่อป้องกัน race condition
		err := tx.QueryRow(ctx,
			`SELECT capacity, COALESCE(current_student, '{}'::text[]), state
			 FROM course WHERE course_id = $1 FOR UPDATE`,
			courseID,
		).Scan(&capacity, &currentStudents, &state)

		if err != nil {
			return EnrollmentResponse{
				Success: false,
				Error:   fmt.Sprintf("Course ID %d not found", courseID),
			}
		}

		// ตรวจสอบว่า course ถูกปิดหรือไม่
		if state == "closed" {
			return EnrollmentResponse{
				Success: false,
				Error:   fmt.Sprintf("Course ID %d is closed", courseID),
			}
		}

		// ตรวจสอบว่ามีที่นั่งเหลือหรือไม่
		if len(currentStudents) >= capacity {
			return EnrollmentResponse{
				Success: false,
				Error:   fmt.Sprintf("Course ID %d is full", courseID),
			}
		}

		// ตรวจสอบว่า student ลงวิชานี้ไปแล้วหรือยัง
		studentIDStr := fmt.Sprintf("%d", msg.StudentID)
		for _, existingStudent := range currentStudents {
			if existingStudent == studentIDStr {
				return EnrollmentResponse{
					Success: false,
					Error:   fmt.Sprintf("Student %d already enrolled in course %d", msg.StudentID, courseID),
				}
			}
		}

		// เพิ่ม student เข้า course
		_, err = tx.Exec(ctx,
			`UPDATE course SET current_student = array_append(current_student, $1) WHERE course_id = $2`,
			studentIDStr, courseID,
		)

		if err != nil {
			return EnrollmentResponse{
				Success: false,
				Error:   fmt.Sprintf("Failed to update course %d: %v", courseID, err),
			}
		}

		// ถ้าเต็มแล้วให้ปิด course
		if len(currentStudents)+1 >= capacity {
			_, err = tx.Exec(ctx,
				`UPDATE course SET state = 'closed' WHERE course_id = $1`,
				courseID,
			)
			if err != nil {
				log.Printf("Warning: Failed to close course %d: %v", courseID, err)
			}
		}
	}

	// Commit transaction
	err = tx.Commit(ctx)
	if err != nil {
		return EnrollmentResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to commit transaction: %v", err),
		}
	}

	return EnrollmentResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully enrolled student %d in courses %v", msg.StudentID, msg.CourseIDs),
	}
}

func main() {
	registerConsul("course-service", 8000)

	// เชื่อมต่อ read และ write databases
	readConn := connectToReadDB()
	writeConn := connectToWriteDB()
	defer readConn.Close(context.Background())
	defer writeConn.Close(context.Background())

	dbConns := &DBConnections{
		ReadConn:  readConn,
		WriteConn: writeConn,
	}

	// เชื่อมต่อ RabbitMQ
	rabbitConn, rabbitChannel := connectToRabbitMQ()
	defer rabbitConn.Close()
	defer rabbitChannel.Close()

	// เริ่ม consumer สำหรับรับข้อความจาก enrollment
	go startEnrollmentConsumer(writeConn, rabbitChannel)

	r := SetupRouter(dbConns)
	log.Println("Course Service started on port 8000")
	r.Run(":8000") // รันที่ localhost:8000
}
