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
	Name  string
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
			{Type: "step", Name: "Start"},
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
