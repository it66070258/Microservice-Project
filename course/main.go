package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/sony/gobreaker"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	amqp "github.com/rabbitmq/amqp091-go"
)

const exchangeName = "enrollment_events"

var rabbitChannel *amqp.Channel

type EnrollmentEvent struct {
	StudentID int   `json:"student_id"`
	CourseIDs []int `json:"course_ids"`
}

func connectRabbitMQ() {

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@rabbitmq:5672/"
	}

	var conn *amqp.Connection
	var err error

	for i := 0; i < 10; i++ {

		conn, err = amqp.Dial(rabbitURL)

		if err == nil {
			break
		}

		log.Println("RabbitMQ retry...")
		time.Sleep(5 * time.Second)
	}

	if err != nil {
		log.Fatal("RabbitMQ connection failed")
	}

	rabbitChannel, err = conn.Channel()
	if err != nil {
		log.Fatal(err)
	}

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

	log.Println("RabbitMQ connected")
}

func startConsumer(dbConns *DBConnections) {

	q, err := rabbitChannel.QueueDeclare(
		"",
		false,
		true,
		true,
		false,
		nil,
	)

	if err != nil {
		log.Fatal(err)
	}

	err = rabbitChannel.QueueBind(
		q.Name,
		"",
		exchangeName,
		false,
		nil,
	)

	if err != nil {
		log.Fatal(err)
	}

	msgs, err := rabbitChannel.Consume(
		q.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
	)

	if err != nil {
		log.Fatal(err)
	}

	go func() {

		for d := range msgs {

			var event EnrollmentEvent

			err := json.Unmarshal(d.Body, &event)

			if err != nil {
				log.Println("JSON decode error:", err)
				continue
			}

			log.Println("Course Event Received")

			for _, courseID := range event.CourseIDs {

				studentStr := strconv.Itoa(event.StudentID)

				_, err := dbConns.WriteConn.Exec(
					context.Background(),
					`UPDATE course 
					 SET current_student =
					 CASE
						WHEN $1 = ANY(current_student) THEN current_student
						ELSE array_append(current_student,$1)
					 END
					 WHERE course_id=$2`,
					studentStr,
					courseID,
				)

				if err != nil {
					log.Println("Update course error:", err)
				}

				_, err = dbConns.WriteConn.Exec(
					context.Background(),
					`UPDATE course
					 SET state='closed'
					 WHERE course_id=$1
					 AND array_length(current_student,1) >= capacity`,
					courseID,
				)

				if err != nil {
					log.Println("Capacity update error:", err)
				}
			}
		}
	}()
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

type DBConnections struct {
	ReadConn  *pgx.Conn
	WriteConn *pgx.Conn
}

func connectToReadDB() *pgx.Conn {

	host := os.Getenv("DB_READ_HOST")
	if host == "" {
		host = "postgres"
	}

	connStr := fmt.Sprintf(
		"user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable",
		host,
	)

	conn, err := pgx.Connect(context.Background(), connStr)

	if err != nil {
		log.Fatal("Unable to connect to READ database:", err)
	}

	log.Println("READ DB connected")

	return conn
}

func connectToWriteDB() *pgx.Conn {

	host := os.Getenv("DB_WRITE_HOST")
	if host == "" {
		host = "postgres"
	}

	connStr := fmt.Sprintf(
		"user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable",
		host,
	)

	conn, err := pgx.Connect(context.Background(), connStr)

	if err != nil {
		log.Fatal("Unable to connect to WRITE database:", err)
	}

	log.Println("WRITE DB connected")

	return conn
}

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

	r.Use(PrometheusMiddleware())

	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	readCB := gobreaker.NewCircuitBreaker(
		gobreaker.Settings{
			Name:     "read-db",
			Interval: time.Minute,
			Timeout:  30 * time.Second,
		},
	)

	r.GET("/courses", func(c *gin.Context) {

		var courses []Course

		_, err := readCB.Execute(func() (interface{}, error) {

			rows, err := dbConns.ReadConn.Query(
				context.Background(),
				`SELECT course_id,subject,credit,section,day_of_week,start_time,end_time,capacity,state,current_student,prerequisite FROM course`,
			)

			if err != nil {
				return nil, err
			}

			defer rows.Close()

			for rows.Next() {

				var course Course

				rows.Scan(
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

				courses = append(courses, course)
			}

			return courses, nil
		})

		if err != nil {

			c.JSON(500, gin.H{"error": err.Error()})

			return
		}

		c.JSON(200, courses)
	})

	return r
}

func main() {

	registerConsul("course-service", 8000)

	readConn := connectToReadDB()
	writeConn := connectToWriteDB()

	defer readConn.Close(context.Background())
	defer writeConn.Close(context.Background())

	dbConns := &DBConnections{
		ReadConn:  readConn,
		WriteConn: writeConn,
	}

	connectRabbitMQ()

	startConsumer(dbConns)

	r := SetupRouter(dbConns)

	log.Println("Course Service running :8000")

	r.Run(":8000")
}
