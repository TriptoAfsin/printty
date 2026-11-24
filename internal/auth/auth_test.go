package auth

import "testing"

func TestPassword(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") {
		t.Error("right password rejected")
	}
	if CheckPassword(h, "wrong horse!") {
		t.Error("wrong password accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Error("short password accepted")
	}
}

func TestSign(t *testing.T) {
	s := Sign("k", "approve:abc")
	if !Verify("k", "approve:abc", s) || Verify("k", "approve:abd", s) || Verify("j", "approve:abc", s) {
		t.Error("signature check wrong")
	}
}
