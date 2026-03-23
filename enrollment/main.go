package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
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

type EnrollmentRequest struct {
	StudentID int   `json:"student_id" binding:"required"`
	CourseIDs []int `json:"course_ids" binding:"required"`
}

type CourseDB struct {
	ID              int
	Credit          int
	Capacity        int
	CurrentStudents []string
	Prerequisite    []string
	DayOfWeek       string
	StartTime       time.Time
	EndTime         time.Time
	State           string
}

const queueName = "enrollment_queue"

type DBConnections struct {
	ReadConn  *sql.DB
	WriteConn *sql.DB
}

func connectToReadDB() *sql.DB {
	host := os.Getenv("DB_READ_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("READ database ping failed:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL READ database (Enrollment)!")
	return db
}

func connectToWriteDB() *sql.DB {
	host := os.Getenv("DB_WRITE_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatal("WRITE database ping failed:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL WRITE database (Enrollment)!")
	return db
}

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

	_, err = rabbitChannel.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		log.Fatal("Queue Declaration Error:", err)
	}

	return rabbitConn, rabbitChannel
}

func SetupRouter(dbConns *DBConnections, rabbitChannel *amqp.Channel) *gin.Engine {
	r := gin.Default()

	r.Use(PrometheusMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

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

	writeSettings := gobreaker.Settings{
		Name:        "Database-Write-Operations",
		MaxRequests: 3,
		Interval:    5 * time.Second,
		Timeout:     5 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures > 2
		},
	}
	writeCircuitBreaker := gobreaker.NewCircuitBreaker(writeSettings)

	r.POST("/enroll", func(c *gin.Context) {
		var req EnrollmentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		_, err := readCircuitBreaker.Execute(func() (interface{}, error) {
			return canEnroll(dbConns.ReadConn, req.StudentID, req.CourseIDs)
		})

		if err != nil {
			if err == gobreaker.ErrOpenState {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ระบบขดของชวคราว (Circuit Breaker Open)"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		body, _ := json.Marshal(req)
		_, err = writeCircuitBreaker.Execute(func() (interface{}, error) {
			return nil, rabbitChannel.PublishWithContext(context.Background(), "", queueName, false, false, amqp.Publishing{
				DeliveryMode: amqp.Persistent,
				ContentType:  "application/json",
				Body:         body,
			})
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ไมสามารถสงขอมลเขาควได"})
			return
		}

		c.JSON(http.StatusAccepted, gin.H{"message": "รบคำขอลงทะเบยนเรยบรอยแลว กำลงดำเนนการ..."})
	})

	return r
}

func startWorker(db *sql.DB, rabbitChannel *amqp.Channel) {
	msgs, err := rabbitChannel.Consume(queueName, "", false, false, false, false, nil)
	if err != nil {
		log.Fatal(err)
	}

	for d := range msgs {
		var req EnrollmentRequest
		json.Unmarshal(d.Body, &req)

		log.Printf("Worker: กำลงประมวลผลการลงทะเบยน นกเรยนรหส %d", req.StudentID)

		err := processTransaction(db, req)
		if err != nil {
			log.Printf("Worker Error: %v", err)
			d.Nack(false, true)
		} else {
			log.Printf("Worker Success: นกเรยนรหส %d ลงทะเบยนสำเรจ", req.StudentID)
			d.Ack(false)
		}
	}
}

func processTransaction(db *sql.DB, req EnrollmentRequest) error {
	courses, err := canEnroll(db, req.StudentID, req.CourseIDs)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, crs := range courses {
		_, err = tx.Exec("UPDATE course SET current_student = array_append(current_student, $1) WHERE course_id = $2", strconv.Itoa(req.StudentID), crs.ID)
		if err != nil {
			return err
		}

		if len(crs.CurrentStudents)+1 >= crs.Capacity {
			_, err = tx.Exec("UPDATE course SET state = 'closed' WHERE course_id = $1", crs.ID)
			if err != nil {
				return err
			}
		}
	}

	var exists bool
	tx.QueryRow("SELECT EXISTS(SELECT 1 FROM enrollment WHERE student_id = $1)", req.StudentID).Scan(&exists)

	if exists {
		_, err = tx.Exec("UPDATE enrollment SET course_id = array_cat(course_id, $1) WHERE student_id = $2", pq.Array(req.CourseIDs), req.StudentID)
	} else {
		_, err = tx.Exec("INSERT INTO enrollment (student_id, course_id) VALUES ($1, $2)", req.StudentID, pq.Array(req.CourseIDs))
	}

	if err != nil {
		return err
	}
	return tx.Commit()
}

func canEnroll(db *sql.DB, studentID int, ids []int) ([]CourseDB, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("ไม่มีรายวิชาที่ต้องลงทะเบียน")
	}

	// 1. ดึงข้อมูลนักเรียนเพื่อตรวจสอบวิชาที่ผ่านแล้ว (Prerequisite)
	var gradedSubjects pq.StringArray
	err := db.QueryRow("SELECT COALESCE(graded_subject, '{}'::text[]) FROM student WHERE student_id = $1", studentID).Scan(pq.Array(&gradedSubjects))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("ไม่พบข้อมูลนักเรียนรหัส %d ในระบบ", studentID)
		}
		return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงข้อมูลนักเรียน: %v", err)
	}

	gradedMap := make(map[string]bool)
	for _, sub := range gradedSubjects {
		gradedMap[sub] = true
	}

	// 2. ดึงข้อมูลวิชาที่ร้องขอลงทะเบียนใหม่ (ตรวจสอบ Capacity / State / Prerequisite)
	var newCourses []CourseDB
	rows, err := db.Query(`SELECT course_id, credit, capacity, COALESCE(current_student, '{}'::text[]), COALESCE(prerequisite, '{}'::text[]), day_of_week, start_time, end_time, state 
		FROM course WHERE course_id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงข้อมูลรายวิชา: %v", err)
	}
	defer rows.Close()

	uniqueCheck := make(map[int]bool)
	totalNewCredit := 0

	for rows.Next() {
		var c CourseDB
		var currentStudents, prerequisites pq.StringArray
		if err := rows.Scan(&c.ID, &c.Credit, &c.Capacity, &currentStudents, &prerequisites, &c.DayOfWeek, &c.StartTime, &c.EndTime, &c.State); err != nil {
			return nil, fmt.Errorf("เกิดข้อผิดพลาดในการอ่านข้อมูลวิชา: %v", err)
		}
		c.CurrentStudents = currentStudents
		c.Prerequisite = prerequisites

		// ป้องกันการส่งรายวิชาเดิมเบิ้ลมาใน Request เดียวกัน
		if uniqueCheck[c.ID] {
			return nil, fmt.Errorf("ไม่อนุญาตให้ระบุวิชารหัส %d ซ้ำกันในคำขอเดียว", c.ID)
		}
		uniqueCheck[c.ID] = true

		if c.State == "closed" {
			return nil, fmt.Errorf("วิชารหัส %d ปิดรับลงทะเบียนแล้ว (State: Closed)", c.ID)
		}
		if len(c.CurrentStudents) >= c.Capacity {
			return nil, fmt.Errorf("วิชารหัส %d ที่นั่งเต็มแล้ว (%d/%d)", c.ID, len(c.CurrentStudents), c.Capacity)
		}
		for _, reqSub := range c.Prerequisite {
			if !gradedMap[reqSub] {
				return nil, fmt.Errorf("นักเรียนยังไม่ผ่านวิชาบังคับก่อนหน้า (%s) สำหรับวิชารหัส %d", reqSub, c.ID)
			}
		}

		newCourses = append(newCourses, c)
		totalNewCredit += c.Credit
	}

	if len(newCourses) != len(ids) {
		return nil, fmt.Errorf("มีรายวิชาที่ระบุไม่ถูกต้องหรือไม่มีอยู่จริงในระบบ")
	}

	// 3. ดึงประวัติที่ลงไปแล้วของนักเรียน เพื่อเช็คหน่วยกิตรวม, เวลาชน, ป้องกันการลงวิชาเดิมซ้ำ
	var existingCourseIDsInt64 []int64
	err = db.QueryRow("SELECT COALESCE(course_id, '{}'::int[]) FROM enrollment WHERE student_id = $1", studentID).Scan(pq.Array(&existingCourseIDsInt64))
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงประวัติการลงทะเบียน: %v", err)
	}

	var existingCourses []CourseDB
	totalExistingCredit := 0

	if len(existingCourseIDsInt64) > 0 {
		eRows, err := db.Query(`SELECT course_id, credit, day_of_week, start_time, end_time 
			FROM course WHERE course_id = ANY($1)`, pq.Array(existingCourseIDsInt64))
		if err != nil {
			return nil, fmt.Errorf("เกิดข้อผิดพลาดในการดึงข้อมูลวิชาที่เคยลง: %v", err)
		}
		defer eRows.Close()

		for eRows.Next() {
			var c CourseDB
			if err := eRows.Scan(&c.ID, &c.Credit, &c.DayOfWeek, &c.StartTime, &c.EndTime); err != nil {
				return nil, fmt.Errorf("เกิดข้อผิดพลาดในการอ่านข้อมูลวิชาที่เคยลง: %v", err)
			}

			// เช็คว่าวิชาที่ขอใหม่ ไปซ้ำกับวิชาที่เคยมีในตารางแล้วหรือไม่
			if uniqueCheck[c.ID] {
				return nil, fmt.Errorf("วิชารหัส %d เคยได้รับการลงทะเบียนและบันทึกไว้ในระบบแล้ว", c.ID)
			}

			existingCourses = append(existingCourses, c)
			totalExistingCredit += c.Credit
		}
	}

	// 4. ตรวจสอบเงื่อนไขลงทะเบียนเกิน 21 หน่วยกิต
	if totalNewCredit+totalExistingCredit > 21 {
		return nil, fmt.Errorf("หน่วยกิตการลงทะเบียนรวมเกิน 21 (ปัจจุบันมี %d หน่วยกิต, ขอเพิ่มใหม่ %d หน่วยกิต)", totalExistingCredit, totalNewCredit)
	}

	// 5. ตรวจสอบการทับซ้อนของตารางเรียน (Schedule Overlap) ระหว่างวิชาเดิมและวิชาใหม่
	allClasses := append(existingCourses, newCourses...)
	for i := 0; i < len(allClasses); i++ {
		for j := i + 1; j < len(allClasses); j++ {
			c1 := allClasses[i]
			c2 := allClasses[j]

			if c1.DayOfWeek != "" && c1.DayOfWeek == c2.DayOfWeek {
				// แปลงเวลาให้เป็นรูปแบบ HH:MM:SS เพื่อการเปรียบเทียบ string ปกติ (ปลอดภัยสำหรับเวลา 24 ชั่วโมง)
				s1 := c1.StartTime.Format("15:04:05")
				e1 := c1.EndTime.Format("15:04:05")
				s2 := c2.StartTime.Format("15:04:05")
				e2 := c2.EndTime.Format("15:04:05")

				// เงื่อนไขเวลาครอบเกี่ยวกัน (Start1 < End2 และ End1 > Start2)
				if s1 < e2 && e1 > s2 {
					return nil, fmt.Errorf("เวลาเรียนทับซ้อนกันวัน %s: วิชารหัส %d (%s-%s) ชนกับ วิชารหัส %d (%s-%s)",
						c1.DayOfWeek, c1.ID, s1, e1, c2.ID, s2, e2)
				}
			}
		}
	}

	return newCourses, nil
}

func main() {
	registerConsul("enrollment-service", 8002)

	readConn := connectToReadDB()
	writeConn := connectToWriteDB()
	defer readConn.Close()
	defer writeConn.Close()

	dbConns := &DBConnections{
		ReadConn:  readConn,
		WriteConn: writeConn,
	}

	rabbitConn, rabbitChannel := connectToRabbitMQ()
	defer rabbitConn.Close()
	defer rabbitChannel.Close()

	go startWorker(dbConns.WriteConn, rabbitChannel)

	r := SetupRouter(dbConns, rabbitChannel)

	log.Println("Enrollment Service เริ่มทำงานที่พอร์ต :8002")
	r.Run(":8002")
}
