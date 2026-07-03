package auth

import (
	"path/filepath"
	"testing"

	"together/internal/db"
)

func TestHashVerify(t *testing.T) {
	h, s := Hash("secret")
	if !Verify("secret", h, s) {
		t.Fatal("verify should pass")
	}
	if Verify("wrong", h, s) {
		t.Fatal("verify should fail")
	}
}

func TestSessionRoundtrip(t *testing.T) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	defer d.Close()
	if err := Seed(d, "admin", "pw"); err != nil {
		t.Fatal(err)
	}
	u, err := Login(d, "admin", "pw")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := CreateSession(d, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UserByToken(d, tok)
	if err != nil || got.Username != "admin" || got.Role != "admin" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := Login(d, "admin", "nope"); err == nil {
		t.Fatal("bad password must fail")
	}
}
