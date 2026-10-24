package markets

import (
	"fmt"
	"strings"
	"testing"
)

func TestMarkSixIndependentPositionContracts(t *testing.T) {
	count := 0
	for _, m := range Catalog("lhc") {
		if !strings.HasPrefix(m.ID, "lhc6.position_special.") {
			continue
		}
		count++
		oldID := strings.Replace(m.ID, "lhc6.position_special.", "lhc6.", 1)
		old := v6Template(t, oldID)
		if m.UIGroup != "position_numbers" || old.UIGroup != "position_faces" {
			t.Fatal("not split", m, old)
		}
		for n := 1; n <= 49; n++ {
			draw := v6WithNumber(m.Positions[0], n)
			if v6Eval(t, m.ID, nil, draw) != v6Eval(t, oldID, nil, draw) {
				t.Fatal("different position semantics", m.ID, n)
			}
		}
	}
	if count != 78 {
		t.Fatal("missing independent two-sided/color contracts", count)
	}
}

func TestMarkSixHalfDigitAndNewCombinations(t *testing.T) {
	for _, color := range []string{"red", "blue", "green"} {
		for _, face := range []string{"odd", "even"} {
			for n := 1; n <= 49; n++ {
				want := v6Want(v6Color(n) == color && ((n/10+n%10)%2 == 1) == (face == "odd"))
				if n == 49 {
					want = Evaluation{Outcome: "push"}
				}
				v6Check(t, "lhc6.special.halfdigit."+color+"_"+face, nil, v6WithNumber(7, n), want)
			}
		}
	}
	draw := []int{1, 13, 25, 37, 49, 2, 14} // 2026: 马、蛇
	for count := 2; count <= 4; count++ {
		values := make([]int, count)
		for i := range values {
			values[i] = i
		}
		id := fmt.Sprintf("lhc6.miss_zodiac.%d", count)
		v6Check(t, id, values, draw, v6Want(true))
		values[0] = 6
		v6Check(t, id, values, draw, v6Want(false))
	}
	values := []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 15, 16}
	v6Check(t, "lhc6.miss.12", values, draw, v6Want(true))
	values[0] = 49
	v6Check(t, "lhc6.miss.12", values, draw, v6Want(false))
}
