#!/bin/bash

set -e

mkdir -p tests

########################################
# AUTH TESTS
########################################

cat << 'EOF' > tests/auth_test.go
package tests

import (
	"testing"
)

type User struct {
	Email string
	Password string
}

func Authenticate(u User) bool {
	if u.Email == "" || u.Password == "" {
		return false
	}
	return true
}

func TestAuthenticateSuccess(t *testing.T) {

	u := User{
		Email: "user@test.com",
		Password: "secret",
	}

	if !Authenticate(u) {
		t.Fatal("expected authentication success")
	}
}

func TestAuthenticateFail(t *testing.T) {

	u := User{}

	if Authenticate(u) {
		t.Fatal("expected authentication failure")
	}
}
EOF


########################################
# BOARD TESTS
########################################

cat << 'EOF' > tests/board_test.go
package tests

import (
	"testing"
)

type Card struct {
	Title string
	Status string
}

func MoveCard(c *Card, status string) {
	c.Status = status
}

func TestMoveCard(t *testing.T) {

	card := Card{
		Title: "Test Task",
		Status: "todo",
	}

	MoveCard(&card, "doing")

	if card.Status != "doing" {
		t.Fatalf("expected doing got %s", card.Status)
	}
}

func TestCreateCard(t *testing.T) {

	card := Card{
		Title: "Example",
		Status: "todo",
	}

	if card.Title == "" {
		t.Fatal("title should not be empty")
	}
}
EOF


########################################
# BUILDER TESTS
########################################

cat << 'EOF' > tests/builder_test.go
package tests

import (
	"encoding/json"
	"testing"
)

type Node struct {
	Type string
	Name string
}

type Workflow struct {
	Name string
	Nodes []Node
}

func TestAddNode(t *testing.T) {

	w := Workflow{
		Name: "Test Workflow",
	}

	n := Node{
		Type: "step",
		Name: "Send Email",
	}

	w.Nodes = append(w.Nodes, n)

	if len(w.Nodes) != 1 {
		t.Fatal("node not added")
	}
}

func TestWorkflowJSONExport(t *testing.T) {

	w := Workflow{
		Name: "Test",
		Nodes: []Node{
			{Type:"step", Name:"Start"},
		},
	}

	data, err := json.Marshal(w)

	if err != nil {
		t.Fatal(err)
	}

	if len(data) == 0 {
		t.Fatal("json export empty")
	}
}
EOF


########################################
# API TESTS
########################################

cat << 'EOF' > tests/api_test.go
package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIEndpoint(t *testing.T) {

	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d", w.Code)
	}
}

func TestAPIPost(t *testing.T) {

	req := httptest.NewRequest("POST", "/api/card", nil)
	w := httptest.NewRecorder()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatal("wrong method")
		}
		w.WriteHeader(201)
	})

	handler.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatal("expected created")
	}
}
EOF


########################################
# DATABASE TESTS
########################################

cat << 'EOF' > tests/db_test.go
package tests

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSQLiteMemoryDB(t *testing.T) {

	db, err := sql.Open("sqlite3", ":memory:")

	if err != nil {
		t.Fatal(err)
	}

	defer db.Close()

	_, err = db.Exec(`
	CREATE TABLE cards(
		id INTEGER PRIMARY KEY,
		title TEXT
	)
	`)

	if err != nil {
		t.Fatal(err)
	}

	_, err = db.Exec(`INSERT INTO cards(title) VALUES("Test")`)

	if err != nil {
		t.Fatal(err)
	}

	row := db.QueryRow(`SELECT title FROM cards WHERE id=1`)

	var title string

	err = row.Scan(&title)

	if err != nil {
		t.Fatal(err)
	}

	if title != "Test" {
		t.Fatal("wrong data returned")
	}
}
EOF


########################################
# INTEGRATION TEST
########################################

cat << 'EOF' > tests/integration_test.go
package tests

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFullRequestFlow(t *testing.T) {

	mux := http.NewServeMux()

	mux.HandleFunc("/board", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})

	req := httptest.NewRequest("GET", "/board", nil)

	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatal("board endpoint failed")
	}
}
EOF


echo "Test suite created."

echo ""
echo "Run tests with:"
echo "go test ./tests -v"
