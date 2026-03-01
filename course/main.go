package main

// dependency
import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// เชื่อม database ใน postgres
func connectToDB() *pgx.Conn {
	connStr := "user=postgres password=1234 host=localhost port=5432 dbname=register sslmode=disable"
	conn, err := pgx.Connect(context.Background(), connStr)
	if err != nil {
		log.Fatal("Unable to connect to database:", err)
	}
	fmt.Println("Successfully connected to PostgreSQL!")

	err = conn.Ping(context.Background())
	if err != nil {
		log.Fatal("Database ping failed:", err)
	}

	return conn
}

// สร้างประเภทตัวแปร
type Course struct {
	CourseID     int       `json:"course_id"`
	Subject      string    `json:"subject"`
	Credit       int       `json:"credit"`
	Section      []string  `json:"section"`
	DayOfWeek    string    `json:"day_of_week"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Capacity     int       `json:"capacity"`
	State        string    `json:"state"`
	Prerequisite *string   `json:"prerequisite"`
}

func main() {
	conn := connectToDB()
	defer conn.Close(context.Background())

	r := gin.Default()

	// ดึง course ออกมาทั้งหมด
	r.GET("/courses", func(c *gin.Context) {
		rows, err := conn.Query(context.Background(), `SELECT "course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "prerequisite" FROM "Course"`)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query courses: " + err.Error()})
			return
		}
		defer rows.Close()

		var courses []Course
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
				&course.Prerequisite,
			)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan course: " + err.Error()})
				return
			}
			courses = append(courses, course)
		}

		c.JSON(http.StatusOK, courses)
	})

	// ดึง course ตัวเดียว
	r.GET("/courses/:id", func(c *gin.Context) {
		id := c.Param("id")

		var course Course
		err := conn.QueryRow(context.Background(),
			`SELECT "course_id", "subject", "credit", "section", "day_of_week", "start_time", "end_time", "capacity", "state", "prerequisite" FROM "Course" WHERE "course_id" = $1`,
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
			&course.Prerequisite,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Course not found"})
			return
		}

		c.JSON(http.StatusOK, course)
	})

	r.Run(":8000") // รันที่ localhost:8000

	fmt.Println("Course Service started on port 8000") // เช็ค

}
