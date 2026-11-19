package docker

import (
	"context"
	"slices"
	"testing"
)

// Only the running containers are asked for (docker stats fails on a
// stopped one), and the percentages are parsed.
func TestStats(t *testing.T) {
	m, r, _ := newManager(t, func(args []string) (Result, error) {
		switch args[0] {
		case "ps":
			return Result{Stdout: []byte(`{"Names":"frankenphp-8.4","State":"running","Status":"Up"}
{"Names":"franken_redis","State":"exited","Status":"Exited (0)"}`)}, nil
		case "stats":
			return Result{Stdout: []byte(`{"Name":"frankenphp-8.4","CPUPerc":"12.50%","MemPerc":"3.2%","MemUsage":"52.3MiB / 7.66GiB","NetIO":"1kB / 2kB","BlockIO":"0B / 0B"}
not json`)}, nil
		}
		return Result{}, nil
	})
	stats, err := m.Stats(context.Background(), "frankenphp-8.4", "franken_redis", "franken_mariadb")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.argLines(); !slices.Contains(got, "stats --no-stream --format json frankenphp-8.4") {
		t.Errorf("docker calls: %v", got)
	}
	want := Stat{Name: "frankenphp-8.4", CPUPercent: 12.5, MemPercent: 3.2, MemUsage: "52.3MiB / 7.66GiB",
		NetIO: "1kB / 2kB", BlockIO: "0B / 0B"}
	if len(stats) != 1 || stats[0] != want {
		t.Errorf("stats = %+v", stats)
	}

	// Nothing running: no stats call
	m, r, _ = newManager(t, nil)
	if stats, err := m.Stats(context.Background(), "franken_redis"); err != nil || len(stats) != 0 || len(r.calls) != 1 {
		t.Errorf("stats %v, %v, calls %v", stats, err, r.argLines())
	}
}
