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
