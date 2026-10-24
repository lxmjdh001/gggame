package main

import "testing"

func TestInitialAdminPasswordMatchesNewLengthPolicy(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		password, err := initialAdminPassword()
		if err != nil {
			t.Fatal(err)
		}
		if len(password) != 16 || seen[password] {
			t.Fatal("bootstrap password has wrong length or repeats")
		}
		seen[password] = true
	}
}
