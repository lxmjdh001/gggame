package apikey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistedCredential(t *testing.T) {
	t.Setenv("GAME_API_TOKEN", "")
	p := filepath.Join(t.TempDir(), "secret")
	if _, err := Load(p, false); err == nil {
		t.Fatal("missing accepted")
	}
	first, err := Load(p, true)
	if err != nil || len(first) != 64 {
		t.Fatal(err)
	}
	second, err := Load(p, true)
	if err != nil || first != second {
		t.Fatal("key changed", err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
	t.Setenv("GAME_API_TOKEN", "configured-secret")
	if key, err := Load(p, false); err != nil || key != "configured-secret" {
		t.Fatal("environment precedence", err)
	}
}
