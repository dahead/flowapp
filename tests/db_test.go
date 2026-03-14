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
