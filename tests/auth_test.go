package tests

import (
	"testing"
)

type User struct {
	Email    string
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
		Email:    "user@test.com",
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
