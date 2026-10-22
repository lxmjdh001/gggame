package generator

import (
	"errors"
	"starlight/gameapi/pkg/model"
	"testing"
	"time"
)

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
func TestElevenGroupRules(t *testing.T) {
	at := time.Now().Add(-time.Minute).Truncate(time.Minute).Add(time.Second)
	for i := 0; i < 200; i++ {
		d, err := New().Generate(at)
		if err != nil {
			t.Fatal(err)
		}
		if err = model.Validate(d); err != nil {
			t.Fatal(err)
		}
		if len(d.Groups) != 11 {
			t.Fatal("expected eleven groups")
		}
		if len(d.Groups["g4"]) != 9 {
			t.Fatal("g4 must have nine numbers")
		}
	}
}
func TestEntropyFailureAndWrongSchedule(t *testing.T) {
	at := time.Now().Add(-time.Minute).Truncate(time.Minute).Add(time.Second)
	if _, err := (&Generator{Reader: brokenReader{}}).Generate(at); err == nil {
		t.Fatal("entropy failures must not publish numbers")
	}
	if _, err := New().Generate(at.Add(time.Second)); err == nil {
		t.Fatal("wrong second accepted")
	}
}
