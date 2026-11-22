package my_sites

import (
	"testing"
	"time"
)

func TestSecurityScheduleDefaultsAndIntervals(t *testing.T) {
	p := defaultSecurityPolicy()
	if e := validateSecurityPolicy(p); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		now      string
		interval int
		want     string
	}{{"2026-11-21T03:59:59+08:00", 24, "2026-11-21T04:00:00+08:00"}, {"2026-11-21T04:00:00+08:00", 24, "2026-11-22T04:00:00+08:00"}, {"2026-11-21T18:13:00+08:00", 6, "2026-11-21T22:00:00+08:00"}, {"2026-11-21T18:13:00+08:00", 48, "2026-11-23T04:00:00+08:00"}}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		p.IntervalHours = c.interval
		if got := nextSecurityRotation(now, p).Format(time.RFC3339); got != c.want {
			t.Errorf("%s interval %d: got %s want %s", c.now, c.interval, got, c.want)
		}
	}
	p.Time = "25:00"
	if validateSecurityPolicy(p) == nil {
		t.Fatal("invalid time accepted")
	}
	p = defaultSecurityPolicy()
	p.IntervalHours = 0
	if validateSecurityPolicy(p) == nil {
		t.Fatal("zero interval accepted")
	}
}
