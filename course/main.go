package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

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

type Course struct {
	CourseID     int       `json:"course_id"`
	Subject      string    `json:"subject"`
	Credit       int       `json:"credit"`
	Section      string    `json:"section"`
	DayOfWeek    string    `json:"day_of_week"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	Capacity     int       `json:"capacity"`
	State        string    `json:"state"`
	Prerequisite string    `json:"prerequisite"`
}

func getCourses(w http.ResponseWriter, r *http.Request) {
	courses := []Course{
		{CourseID: 1, Subject: "Mathematics", Credit: 3, Section: "1", DayOfWeek: "Monday", StartTime: time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC), EndTime: time.Date(0, 1, 1, 12, 0, 0, 0, time.UTC), Capacity: 30, State: "open", Prerequisite: ""},
		{CourseID: 2, Subject: "Physics", Credit: 3, Section: "1", DayOfWeek: "Tuesday", StartTime: time.Date(0, 1, 1, 13, 0, 0, 0, time.UTC), EndTime: time.Date(0, 1, 1, 16, 0, 0, 0, time.UTC), Capacity: 30, State: "open", Prerequisite: "Mathematics"},
		{CourseID: 3, Subject: "Computer Science", Credit: 3, Section: "1", DayOfWeek: "Wednesday", StartTime: time.Date(0, 1, 1, 9, 0, 0, 0, time.UTC), EndTime: time.Date(0, 1, 1, 13, 0, 0, 0, time.UTC), Capacity: 80, State: "open", Prerequisite: ""},
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(courses)
	fmt.Println("Endpoint Hit: returnAllcourses")
}

func main() {
	conn := connectToDB()
	defer conn.Close(context.Background())

	http.HandleFunc("/courses", getCourses)
	fmt.Println("Course Service started on port 8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}
