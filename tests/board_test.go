package tests

import (
	"testing"
)

type Card struct {
	Title  string
	Status string
}

func MoveCard(c *Card, status string) {
	c.Status = status
}

func TestMoveCard(t *testing.T) {

	card := Card{
		Title:  "Test Task",
		Status: "todo",
	}

	MoveCard(&card, "doing")

	if card.Status != "doing" {
		t.Fatalf("expected doing got %s", card.Status)
	}
}

func TestCreateCard(t *testing.T) {

	card := Card{
		Title:  "Example",
		Status: "todo",
	}

	if card.Title == "" {
		t.Fatal("title should not be empty")
	}
}
