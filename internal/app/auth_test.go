package app

import "testing"

func TestPasswordHash(t *testing.T) {
	h := hashPassword("a sufficiently long password")
	if !verifyPassword(h, "a sufficiently long password") {
		t.Fatal("valid password rejected")
	}
	if verifyPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}
