package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

type Student struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Course string `json:"course"`
}

var db *sql.DB

func main() {

	// Read environment variables
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	database := os.Getenv("DB_NAME")

	// Create MySQL connection string
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s",
		user, password, host, port, database,
	)

	// Connect to MySQL
	var err error
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Database connection pool
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Wait for MySQL
	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		log.Println("Waiting for MySQL...")
		time.Sleep(2 * time.Second)
	}

	if err != nil {
		log.Fatalf("MySQL connection failed: %v", err)
	}

	log.Println("MySQL connected successfully!")

	// API routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/students", studentsHandler)
	mux.HandleFunc("/api/students/", studentHandler)

	// HTTP server
	server := &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("Go API listening on port 8081")
	log.Fatal(server.ListenAndServe())
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := db.Ping(); err != nil {
		http.Error(w, "Database unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Backend is healthy")
}

func studentsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getStudents(w)
	case http.MethodPost:
		createStudent(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getStudents(w http.ResponseWriter) {
	rows, err := db.Query(
		"SELECT id, name, email, course FROM students ORDER BY id",
	)
	if err != nil {
		http.Error(w, "Unable to load students", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	students := []Student{}

	for rows.Next() {
		var s Student
		if err := rows.Scan(&s.ID, &s.Name, &s.Email, &s.Course); err != nil {
			http.Error(w, "Unable to read student", http.StatusInternalServerError)
			return
		}
		students = append(students, s)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Database query failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, students)
}

func createStudent(w http.ResponseWriter, r *http.Request) {
	var s Student

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if !validStudent(s) {
		http.Error(w, "Name, email and course are required", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		"INSERT INTO students (name, email, course) VALUES (?, ?, ?)",
		strings.TrimSpace(s.Name),
		strings.TrimSpace(s.Email),
		strings.TrimSpace(s.Course),
	)
	if err != nil {
		http.Error(w, "Unable to create student", http.StatusInternalServerError)
		return
	}

	id, err := result.LastInsertId()
	if err != nil {
		http.Error(w, "Unable to retrieve student ID", http.StatusInternalServerError)
		return
	}

	s.ID = int(id)
	s.Name = strings.TrimSpace(s.Name)
	s.Email = strings.TrimSpace(s.Email)
	s.Course = strings.TrimSpace(s.Course)

	writeJSON(w, http.StatusCreated, s)
}

func studentHandler(w http.ResponseWriter, r *http.Request) {
	idText := strings.TrimPrefix(r.URL.Path, "/api/students/")
	id, err := strconv.Atoi(idText)

	if idText == "" || strings.Contains(idText, "/") || err != nil || id <= 0 {
		http.Error(w, "Invalid student ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		updateStudent(w, r, id)
	case http.MethodDelete:
		deleteStudent(w, id)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func updateStudent(w http.ResponseWriter, r *http.Request, id int) {
	var s Student

	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if !validStudent(s) {
		http.Error(w, "Name, email and course are required", http.StatusBadRequest)
		return
	}

	result, err := db.Exec(
		"UPDATE students SET name = ?, email = ?, course = ? WHERE id = ?",
		strings.TrimSpace(s.Name),
		strings.TrimSpace(s.Email),
		strings.TrimSpace(s.Course),
		id,
	)
	if err != nil {
		http.Error(w, "Unable to update student", http.StatusInternalServerError)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Unable to verify update", http.StatusInternalServerError)
		return
	}

	if affected == 0 {
		var exists bool
		err := db.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM students WHERE id = ?)", id,
		).Scan(&exists)
		if err != nil {
			http.Error(w, "Database query failed", http.StatusInternalServerError)
			return
		}
		if !exists {
			http.Error(w, "Student not found", http.StatusNotFound)
			return
		}
	}

	s.ID = id
	s.Name = strings.TrimSpace(s.Name)
	s.Email = strings.TrimSpace(s.Email)
	s.Course = strings.TrimSpace(s.Course)

	writeJSON(w, http.StatusOK, s)
}

func deleteStudent(w http.ResponseWriter, id int) {
	result, err := db.Exec("DELETE FROM students WHERE id = ?", id)
	if err != nil {
		http.Error(w, "Unable to delete student", http.StatusInternalServerError)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "Unable to verify deletion", http.StatusInternalServerError)
		return
	}

	if affected == 0 {
		http.Error(w, "Student not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func validStudent(s Student) bool {
	return strings.TrimSpace(s.Name) != "" &&
		strings.TrimSpace(s.Email) != "" &&
		strings.TrimSpace(s.Course) != ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(value); err != nil &&
		!errors.Is(err, http.ErrHandlerTimeout) {
		log.Printf("Response encoding error: %v", err)
	}
}
