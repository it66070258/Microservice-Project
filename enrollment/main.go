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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	_ "github.com/lib/pq"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sony/gobreaker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	queueName    = "enrollment_queue"
	exchangeName = "enrollment_events"
)

var (
	db            *sql.DB
	rabbitConn    *amqp.Connection
	rabbitChannel *amqp.Channel

	// Circuit Breaker
	dbBreaker *gobreaker.CircuitBreaker
)

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
	StartTime       string
	EndTime         string
	State           string
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
			Help: "Duration of HTTP requests",
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

		req, _ := http.NewRequest(
			"PUT",
			"http://consul:8500/v1/agent/service/register",
			bytes.NewBuffer(payload),
		)

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

func main() {

	registerConsul("enrollment-service", 8002)

	var err error

	// ---------------- DATABASE ----------------

	host := os.Getenv("DB_HOST")

	if host == "" {
		host = "postgres"
	}

	connStr := fmt.Sprintf(
		"user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable",
		host,
	)

	db, err = sql.Open("postgres", connStr)

	if err != nil {
		log.Fatal(err)
	}

	db.SetConnMaxLifetime(5 * time.Second)
	db.SetMaxIdleConns(0)
	db.SetMaxOpenConns(5)
	defer db.Close()

	// ---------------- CIRCUIT BREAKER ----------------

	dbBreaker = gobreaker.NewCircuitBreaker(
		gobreaker.Settings{
			Name:        "db-breaker",
			MaxRequests: 3,
			Interval:    10 * time.Second,
			Timeout:     5 * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures > 3
			},
		},
	)

	// ---------------- RABBITMQ ----------------

	rabbitURL := os.Getenv("RABBITMQ_URL")

	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	for i := 0; i < 5; i++ {

		rabbitConn, err = amqp.Dial(rabbitURL)

		if err == nil {
			break
		}

		log.Println("RabbitMQ retry...")
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		log.Fatal(err)
	}

	rabbitChannel, err = rabbitConn.Channel()

	if err != nil {
		log.Fatal(err)
	}

	// queue worker

	_, err = rabbitChannel.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Fatal(err)
	}

	// exchange fanout

	err = rabbitChannel.ExchangeDeclare(
		exchangeName,
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Fatal(err)
	}

	// ---------------- WORKER ----------------

	go startWorker()

	// ---------------- GIN ----------------

	r := gin.Default()

	r.Use(PrometheusMiddleware())

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/health", healthHandler)

	r.POST("/enroll", enrollHandler)

	log.Println("Enrollment Service running :8002")

	r.Run(":8002")
}

func healthHandler(c *gin.Context) {

	if err := db.Ping(); err != nil {

		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "unhealthy",
		})

		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "healthy",
	})
}

func enrollHandler(c *gin.Context) {

	var req EnrollmentRequest

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})

		return
	}

	log.Println("breaker state before:", dbBreaker.State())

	_, err := dbBreaker.Execute(func() (interface{}, error) {
		return canEnroll(db, req.StudentID, req.CourseIDs)
	})

	log.Println("breaker state after:", dbBreaker.State())

	if err != nil {

		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "database unavailable",
		})

		return
	}

	body, _ := json.Marshal(req)

	err = rabbitChannel.PublishWithContext(
		context.Background(),
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)

	if err != nil {

		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "queue error",
		})

		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "enrollment processing",
	})
}

func startWorker() {

	msgs, err := rabbitChannel.Consume(
		queueName,
		"",
		false,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Fatal(err)
	}

	for d := range msgs {

		var req EnrollmentRequest

		json.Unmarshal(d.Body, &req)

		err := processTransaction(req)

		if err != nil {

			d.Nack(false, true)

		} else {

			d.Ack(false)
		}
	}
}

func processTransaction(req EnrollmentRequest) error {

	tx, err := db.Begin()

	if err != nil {
		return err
	}

	defer tx.Rollback()

	var exists bool

	tx.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM enrollment WHERE student_id=$1)",
		req.StudentID,
	).Scan(&exists)

	if exists {

		_, err = tx.Exec(
			"UPDATE enrollment SET course_id=array_cat(course_id,$1) WHERE student_id=$2",
			pq.Array(req.CourseIDs),
			req.StudentID,
		)

	} else {

		_, err = tx.Exec(
			"INSERT INTO enrollment (student_id,course_id) VALUES ($1,$2)",
			req.StudentID,
			pq.Array(req.CourseIDs),
		)
	}

	if err != nil {
		return err
	}

	err = tx.Commit()

	if err != nil {
		return err
	}

	// ---------- EVENT FANOUT ----------

	event := map[string]interface{}{
		"student_id": req.StudentID,
		"course_ids": req.CourseIDs,
	}

	body, _ := json.Marshal(event)

	err = rabbitChannel.Publish(
		exchangeName,
		"",
		false,
		false,
		amqp.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)

	if err != nil {
		log.Println("event publish error", err)
	}

	return nil
}

func canEnroll(db *sql.DB, studentID int, ids []int) ([]CourseDB, error) {

	var result []CourseDB

	for _, id := range ids {

		var course CourseDB

		err := db.QueryRow(`
			SELECT course_id, capacity, state
			FROM course
			WHERE course_id=$1
		`, id).Scan(
			&course.ID,
			&course.Capacity,
			&course.State,
		)

		if err != nil {
			log.Println("DB ERROR:", err)
			return nil, err
		}

		result = append(result, course)
	}

	return result, nil
}
