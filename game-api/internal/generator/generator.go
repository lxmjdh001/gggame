package generator

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"starlight/gameapi/pkg/model"
	"time"
)

type Generator struct{ Reader io.Reader }

func New() *Generator { return &Generator{Reader: rand.Reader} }
func (g *Generator) number(n int) (int, error) {
	v, err := rand.Int(g.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}
func (g *Generator) Generate(scheduled time.Time) (model.Draw, error) {
	scheduled = scheduled.In(model.Beijing)
	if scheduled.Second() != 1 || scheduled.Nanosecond() != 0 {
		return model.Draw{}, fmt.Errorf("schedule must be at second 01")
	}
	d := model.Draw{SchemaVersion: model.SchemaVersion, Issue: scheduled.Format(model.IssueLayout), Timezone: "Asia/Shanghai", ScheduledAt: scheduled, Groups: make(map[string][]int)}
	for _, rule := range model.Rules() {
		values := make([]int, rule.Count)
		if rule.AllowRepeat {
			for i := range values {
				v, err := g.number(rule.Max - rule.Min + 1)
				if err != nil {
					return model.Draw{}, err
				}
				values[i] = rule.Min + v
			}
		} else {
			pool := make([]int, rule.Max-rule.Min+1)
			for i := range pool {
				pool[i] = rule.Min + i
			}
			// Fisher-Yates: preserve uniformly shuffled order for positional consumers.
			for i := len(pool) - 1; i > 0; i-- {
				j, err := g.number(i + 1)
				if err != nil {
					return model.Draw{}, err
				}
				pool[i], pool[j] = pool[j], pool[i]
			}
			copy(values, pool[:rule.Count])
		}
		d.Groups[rule.ID] = values
	}
	d.GeneratedAt = time.Now().In(model.Beijing)
	if err := model.Validate(d); err != nil {
		return model.Draw{}, err
	}
	model.Display(&d)
	return d, nil
}
