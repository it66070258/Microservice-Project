package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

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

// โครงสร้างข้อมูลนักเรียน
type Student struct {
	StudentID     int      `json:"student_id"`
	FirstName     string   `json:"first_name"`
	LastName      string   `json:"last_name"`
	Email         string   `json:"email"`
	Password      string   `json:"password,omitempty"` // omitempty เพื่อไม่ให้ส่ง hash password กลับไปใน JSON
	Birthdate     string   `json:"birthdate"`
	Gender        string   `json:"gender"`
	YearLevel     int      `json:"year_level"`
	GradedSubject []string `json:"graded_subject"`
}

// ฟังก์ชันสำหรับ Hash Password
func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// ฟังก์ชันสำหรับเช็ค Password
func checkPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func connectToDB() *pgx.Conn {
	host := os.Getenv("DB_HOST")
	if host == "" {
		host = "localhost"
	}
	connStr := fmt.Sprintf("user=postgres password=1234 host=%s port=5432 dbname=register sslmode=disable", host)
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to database:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL (Student Service)!")
	return conn
}

func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		user := session.Get("user_id")
		if user == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "กรุณา Login ก่อนเข้าใช้งาน"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func SetupRouter(conn *pgx.Conn) *gin.Engine {
	r := gin.Default()

	// ใช้งาน Prometheus Middleware
	r.Use(PrometheusMiddleware())
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	store := cookie.NewStore([]byte("super-secret-key"))
	r.Use(sessions.Sessions("student_session", store))

	// 1. Register พร้อม Hash Password
	r.POST("/register", func(c *gin.Context) {
		var s Student
		if err := c.ShouldBindJSON(&s); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		hashedPassword, err := hashPassword(s.Password)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
			return
		}

		_, err = conn.Exec(context.Background(),
			`INSERT INTO student (student_id, first_name, last_name, email, password, birthdate, gender, year_level, graded_subject) 
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			s.StudentID, s.FirstName, s.LastName, s.Email, hashedPassword, s.Birthdate, s.Gender, s.YearLevel, s.GradedSubject,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Registration failed: " + err.Error()})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"message": "ลงทะเบียนสำเร็จ"})
	})

	// 2. Login พร้อมเช็ค Hash Password
	r.POST("/login", func(c *gin.Context) {
		var loginData struct {
			Email    string `json:"email" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&loginData); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "กรุณากรอกข้อมูลให้ครบ"})
			return
		}

		var studentID int
		var dbPassword string
		err := conn.QueryRow(context.Background(),
			`SELECT student_id, password FROM student WHERE email = $1`, loginData.Email).Scan(&studentID, &dbPassword)

		if err != nil || !checkPasswordHash(loginData.Password, dbPassword) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Email หรือ Password ไม่ถูกต้อง"})
			return
		}

		session := sessions.Default(c)
		session.Set("user_id", studentID)
		session.Save()

		c.JSON(http.StatusOK, gin.H{"message": "Login สำเร็จ", "student_id": studentID})
	})

	r.POST("/logout", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
		session.Save()
		c.JSON(http.StatusOK, gin.H{"message": "Logout สำเร็จ"})
	})

	profile := r.Group("/profile")
	profile.Use(AuthRequired())
	{
		profile.GET("", func(c *gin.Context) {
			userID := sessions.Default(c).Get("user_id")
			var s Student
			err := conn.QueryRow(context.Background(),
				`SELECT student_id, first_name, last_name, email, birthdate, gender, year_level, graded_subject 
				 FROM student WHERE student_id = $1`, userID).Scan(
				&s.StudentID, &s.FirstName, &s.LastName, &s.Email, &s.Birthdate, &s.Gender, &s.YearLevel, &s.GradedSubject,
			)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
				return
			}
			c.JSON(http.StatusOK, s)
		})

		profile.PUT("", func(c *gin.Context) {
			userID := sessions.Default(c).Get("user_id")
			var up Student
			if err := c.ShouldBindJSON(&up); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "ข้อมูลไม่ถูกต้อง"})
				return
			}

			_, err := conn.Exec(context.Background(),
				`UPDATE student SET first_name=$1, last_name=$2, birthdate=$3, gender=$4, year_level=$5 WHERE student_id=$6`,
				up.FirstName, up.LastName, up.Birthdate, up.Gender, up.YearLevel, userID,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Update failed"})
				return
			}
			c.JSON(http.StatusOK, gin.H{"message": "อัปเดตข้อมูลสำเร็จ"})
		})
	}

	return r
}

func main() {
	conn := connectToDB()
	defer conn.Close(context.Background())

	r := SetupRouter(conn)

	port := ":8001" // แยกพอร์ตเป็น 8001 ไม่ให้ชนกับ Course
	fmt.Printf("Student Service started on port %s\n", port)
	r.Run(port)
}
