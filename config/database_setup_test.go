package config

import (
	"net/url"
	"testing"
)

func TestDatabaseSetupValidationAndIPv6(t *testing.T) {
	d := Database{Host: "::1", Port: 5432, User: "user@name", Password: "p@ss:/?#", DBName: "my db", SSLMode: "require"}
	dsn, err := d.ConnectionString()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if u.Host != "[::1]:5432" || password != d.Password || u.User.Username() != d.User || u.Path != "/my db" {
		t.Fatal("connection fields were not escaped correctly")
	}
	for _, bad := range []Database{{}, {Host: "host/path", Port: 5432, User: "u", DBName: "db", SSLMode: "disable"}, {Host: "host", Port: 70000, User: "u", DBName: "db", SSLMode: "disable"}, {Host: "host", Port: 5432, User: "u", DBName: "db", SSLMode: "invalid"}} {
		if _, err := bad.ConnectionString(); err == nil {
			t.Fatal("invalid fields accepted")
		}
	}
}
