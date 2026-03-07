package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/eapache/go-resiliency/breaker"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
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

type StudentDB struct {
	GradedSubjects []string
}

var db *sql.DB
var cb *breaker.Breaker

func main() {
	var err error
	host := getEnv("DB_HOST", "localhost")
	password := getEnv("DB_PASSWORD", "password")
	dbname := getEnv("DB_NAME", "micro")
	connStr := fmt.Sprintf("host=%s port=5432 user=postgres password=%s dbname=%s sslmode=disable", host, password, dbname) // adjust as needed
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	cb = breaker.New(3, 1, 5*time.Second)

	r := gin.Default()
	r.GET("/health", healthHandler)
	r.POST("/enroll", enrollHandler)
	r.Run()
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func healthHandler(c *gin.Context) {
	err := db.Ping()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "healthy"})
}

func enrollHandler(c *gin.Context) {
	var req EnrollmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	courses, err := canEnroll(db, req.StudentID, req.CourseIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// transaction
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db error"})
		return
	}
	defer tx.Rollback()

	// update courses
	for _, crs := range courses {
		_, err = tx.Exec("UPDATE course SET current_student = array_append(current_student, $1) WHERE course_id = $2", strconv.Itoa(req.StudentID), crs.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "update course error"})
			return
		}
		if len(crs.CurrentStudents)+1 >= crs.Capacity {
			_, err = tx.Exec("UPDATE course SET state = 'closed' WHERE course_id = $1", crs.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "update state error"})
				return
			}
		}
	}

	// check if enrollment exists
	var exists bool
	err = cb.Run(func() error {
		return tx.QueryRow("SELECT EXISTS(SELECT 1 FROM enrollment WHERE student_id = $1)", req.StudentID).Scan(&exists)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "check enrollment error"})
		return
	}

	if exists {
		_, err = tx.Exec("UPDATE enrollment SET course_id = array_cat(course_id, $1) WHERE student_id = $2", pq.Array(req.CourseIDs), req.StudentID)
	} else {
		_, err = tx.Exec("INSERT INTO enrollment (student_id, course_id) VALUES ($1, $2)", req.StudentID, pq.Array(req.CourseIDs))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "update enrollment error"})
		return
	}

	err = tx.Commit()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "enrollment successful"})
}

func canEnroll(db *sql.DB, studentID int, ids []int) ([]CourseDB, error) {
	// check duplicate in ids
	seen := make(map[int]bool)
	for _, id := range ids {
		if seen[id] {
			return nil, fmt.Errorf("duplicate course %d in request", id)
		}
		seen[id] = true
	}

	// get student info
	var student StudentDB
	err := cb.Run(func() error {
		s, e := getStudent(db, studentID)
		student = s
		return e
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("student %d not found", studentID)
		}
		return nil, err
	}

	// get current enrollment
	var currentCourses pq.Int64Array
	err = cb.Run(func() error {
		return db.QueryRow("SELECT course_id FROM enrollment WHERE student_id = $1", studentID).Scan(&currentCourses)
	})
	currentCourseIDs := make([]int, len(currentCourses))
	for i, v := range currentCourses {
		currentCourseIDs[i] = int(v)
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	// check duplicate with existing
	for _, id := range ids {
		for _, curr := range currentCourseIDs {
			if id == curr {
				return nil, fmt.Errorf("student already enrolled in course %d", id)
			}
		}
	}

	// get total current credits
	totalCredits := 0
	for _, cid := range currentCourseIDs {
		var c CourseDB
		err := cb.Run(func() error {
			crs, e := getCourse(db, cid)
			c = crs
			return e
		})
		if err != nil {
			return nil, fmt.Errorf("error getting course %d: %v", cid, err)
		}
		totalCredits += c.Credit
	}

	// collect courses
	courses := make([]CourseDB, 0, len(ids))
	for _, id := range ids {
		var c CourseDB
		err := cb.Run(func() error {
			crs, e := getCourse(db, id)
			c = crs
			return e
		})
		if err != nil {
			return nil, fmt.Errorf("course %d not found", id)
		}
		if c.State != "open" {
			return nil, fmt.Errorf("course %d is not open", id)
		}
		if len(c.CurrentStudents) >= c.Capacity {
			return nil, fmt.Errorf("course %d is full", id)
		}
		// check prereq
		for _, pre := range c.Prerequisite {
			found := false
			for _, graded := range student.GradedSubjects {
				if graded == pre {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("missing prerequisite %s for course %d", pre, id)
			}
		}
		courses = append(courses, c)
		totalCredits += c.Credit
	}

	if totalCredits > 22 {
		return nil, fmt.Errorf("total credits %d exceed 22", totalCredits)
	}

	// check time conflict
	for i := 0; i < len(courses); i++ {
		for j := i + 1; j < len(courses); j++ {
			if isConflict(courses[i], courses[j]) {
				return nil, fmt.Errorf("time conflict between %d and %d", courses[i].ID, courses[j].ID)
			}
		}
		// check with current
		for _, currC := range currentCourseIDs {
			var currCourse CourseDB
			err := cb.Run(func() error {
				crs, e := getCourse(db, currC)
				currCourse = crs
				return e
			})
			if err != nil {
				continue
			}
			if isConflict(courses[i], currCourse) {
				return nil, fmt.Errorf("time conflict with existing course %d", currC)
			}
		}
	}

	return courses, nil
}

func getCourse(db *sql.DB, id int) (CourseDB, error) {
	var c CourseDB
	var curr pq.StringArray
	var prereq pq.StringArray
	err := cb.Run(func() error {
		return db.QueryRow("SELECT credit, capacity, current_student, prerequisite, day_of_week, start_time, end_time, state FROM course WHERE course_id = $1", id).Scan(&c.Credit, &c.Capacity, &curr, &prereq, &c.DayOfWeek, &c.StartTime, &c.EndTime, &c.State)
	})
	if err != nil {
		return c, err
	}
	c.CurrentStudents = []string(curr)
	c.Prerequisite = []string(prereq)
	c.ID = id
	return c, nil
}

func getStudent(db *sql.DB, id int) (StudentDB, error) {
	var s StudentDB
	var graded pq.StringArray
	err := cb.Run(func() error {
		return db.QueryRow("SELECT graded_subject FROM student WHERE student_id = $1", id).Scan(&graded)
	})
	if err != nil {
		return s, err
	}
	s.GradedSubjects = []string(graded)
	return s, nil
}

func isConflict(a, b CourseDB) bool {
	return a.DayOfWeek == b.DayOfWeek && a.StartTime == b.StartTime && a.EndTime == b.EndTime
}
