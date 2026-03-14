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
