package auth

import "testing"

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("a very strong password")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyPassword("a very strong password", hash)
	if err != nil || !valid {
		t.Fatalf("valid = %v, err = %v", valid, err)
	}
	valid, err = VerifyPassword("wrong password", hash)
	if err != nil || valid {
		t.Fatalf("wrong password valid = %v, err = %v", valid, err)
	}
}
