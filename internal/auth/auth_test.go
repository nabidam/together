package auth

import (
	"errors"
	"path/filepath"
	"strings"
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
	if err := Seed(d, "admin", "correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	u, err := Login(d, "admin", "correct horse battery staple")
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

func TestSeed_RequiresStrongCredentialsOnFirstBoot(t *testing.T) {
	tests := []struct {
		name string
		user string
		pass string
	}{
		{name: "missing password", user: "seeduser", pass: ""},
		{name: "missing username", pass: "correct horse"},
		{name: "eleven code points", user: "seeduser", pass: "12345678901"},
		{name: "twelve unicode code points", user: "seeduser", pass: "éééééééééééé"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
			defer d.Close()

			err := Seed(d, tt.user, tt.pass)
			if tt.name == "twelve unicode code points" {
				if err != nil {
					t.Fatalf("Seed() error = %v", err)
				}
			} else if !errors.Is(err, ErrSeedCredentials) {
				t.Fatalf("Seed() error = %v, want ErrSeedCredentials", err)
			} else if (tt.user != "" && strings.Contains(err.Error(), tt.user)) || (tt.pass != "" && strings.Contains(err.Error(), tt.pass)) {
				t.Fatalf("Seed() error = %q leaks a supplied credential", err)
			}

			var count int
			if err := d.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if tt.name == "twelve unicode code points" && count != 1 {
				t.Fatalf("users = %d, want 1", count)
			}
			if tt.name != "twelve unicode code points" && count != 0 {
				t.Fatalf("users = %d, want 0", count)
			}
		})
	}
}

func TestSeed_ExistingDatabaseIgnoresCredentials(t *testing.T) {
	d, _ := db.Open(filepath.Join(t.TempDir(), "t.db"))
	defer d.Close()
	const password = "correct horse battery staple"
	if err := Seed(d, "admin", password); err != nil {
		t.Fatal(err)
	}
	if err := Seed(d, "", ""); err != nil {
		t.Fatalf("Seed() on existing database error = %v", err)
	}

	u, err := Login(d, "admin", password)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "admin" || u.Role != "admin" {
		t.Fatalf("user = %+v, want preserved admin", u)
	}
	var count int
	if err := d.QueryRow(`SELECT count(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("users = %d, want 1", count)
	}
}
