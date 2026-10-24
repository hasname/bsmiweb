package collector

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	http "github.com/bogdanfinn/fhttp"
	"github.com/hasname/bsmiweb/internal/db"
)

// Example 4 and Table 5 of "Synchronizing a database to improve freshness".
func TestAllocateMatchesPaper(t *testing.T) {
	freqs := allocate([]float64{1, 2, 3, 4, 5}, 5, 100)
	for i, want := range []float64{1.15, 1.36, 1.35, 1.14, 0} {
		if math.Abs(freqs[i]-want) > 0.005 {
			t.Fatalf("frequencies %v", freqs)
		}
	}
	freqs = allocate([]float64{1, 2}, 5, 1)
	if freqs[0] != 1 || freqs[1] != 1 {
		t.Fatalf("budget above limit: %v", freqs)
	}
}

func TestChangeRate(t *testing.T) {
	if changeRate(0, 0, 0) != 0 || changeRate(3, 1, 0) != 0 {
		t.Fatal("rate without observations")
	}
	unchanged, changed, always := changeRate(10, 0, 10), changeRate(10, 5, 10), changeRate(10, 10, 10)
	if unchanged <= 0 || unchanged >= changed || changed >= always || math.IsInf(always, 0) {
		t.Fatal(unchanged, changed, always)
	}
	if math.Abs(changeRate(10, 5, 20)-changed/2) > 1e-12 {
		t.Fatal("rate does not scale with the check interval")
	}
}

func TestRefreshOrder(t *testing.T) {
	now := time.Date(2026, 10, 25, 4, 0, 0, 0, time.UTC)
	ago := func(days int) time.Time { return now.Add(-time.Duration(days) * 24 * time.Hour) }
	const month = 30 * 86400
	stats := []db.RefreshStat{
		{ID: "R00001", SyncedAt: ago(10)},
		// Changed at every check: too volatile to keep fresh with a small budget.
		{ID: "R00002", SyncedAt: ago(30), Checks: 30, Changes: 30, CheckSeconds: 3 * 86400},
		{ID: "R00003", SyncedAt: ago(4), Checks: 30, Changes: 1, CheckSeconds: month},
		{ID: "R00004", SyncedAt: ago(3), Checks: 30, Changes: 1, CheckSeconds: month},
		{ID: "R00005", SyncedAt: ago(2), Checks: 30, Changes: 1, CheckSeconds: month},
		{ID: "R00006", SyncedAt: ago(1), Checks: 30, Changes: 1, CheckSeconds: month},
	}
	ids := func(order []db.RefreshStat) []string {
		var ids []string
		for _, s := range order {
			ids = append(ids, s.ID)
		}
		return ids
	}
	if got := ids(refreshOrder(stats, now, 2)); !slices.Equal(got, []string{"R00001", "R00003"}) {
		t.Fatalf("limited budget: %v", got)
	}
	// With enough budget, every registration is checked daily, oldest first.
	if got := ids(refreshOrder(stats, now, 10)); !slices.Equal(got, []string{"R00002", "R00001", "R00003", "R00004", "R00005", "R00006"}) {
		t.Fatalf("ample budget: %v", got)
	}
	// Before any checks, the allocation is uniform and the oldest come first.
	fresh := []db.RefreshStat{{ID: "R00001", SyncedAt: ago(1)}, {ID: "R00002", SyncedAt: ago(5)}, {ID: "R00003", SyncedAt: ago(3)}}
	if got := ids(refreshOrder(fresh, now, 2)); !slices.Equal(got, []string{"R00002", "R00003"}) {
		t.Fatalf("no checks: %v", got)
	}
	if refreshOrder(nil, now, 2) != nil || refreshOrder(stats, now, 0) != nil {
		t.Fatal("empty input")
	}
}

func TestRecentChecksSinceLastSync(t *testing.T) {
	s := collectorStore(t)
	ctx := context.Background()
	for _, id := range []string{"R00001", "R00002"} {
		if err := s.SaveRegistration(ctx, &db.Registration{ID: id, Applicant: "old"}); err != nil {
			t.Fatal(err)
		}
	}
	synced := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if _, err := s.DB.Exec("UPDATE registration SET updated_at=?", synced.UnixMilli()); err != nil {
		t.Fatal(err)
	}
	vendor := strings.Replace(emptyFixture(t), "申請人：</label>\n        <div class=\"form-control-static\"><p></p>", "申請人：</label>\n        <div class=\"form-control-static\"><p>new</p>", 1)
	c := mockClient(func(r *http.Request) (string, int) {
		r.ParseForm()
		switch r.URL.String() {
		case certURL:
			if r.Form.Get("q_modDateS") != "115/10/01" {
				t.Fatal(r.Form)
			}
			if r.Form.Get("q_regNo") == "00001" {
				return `<div id="listContainer">共1筆</div>`, 200
			}
			return `<div id="listContainer">共0筆</div>`, 200
		case vendorURL:
			if r.Form.Get("q_regNo") != "00001" {
				t.Fatal("refreshed an unchanged registration")
			}
			return vendor, 200
		}
		t.Fatal(r.URL)
		return "", 404
	})
	j := &Jobs{Client: c, Store: s, Location: time.UTC}
	if err := j.Run(ctx, "recent"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		id, applicant string
		changes       int
	}{{"R00001", "new", 1}, {"R00002", "old", 0}} {
		r, err := s.Registration(ctx, want.id)
		if err != nil || r.Applicant != want.applicant {
			t.Fatalf("%s: %+v %v", want.id, r, err)
		}
	}
	stats, err := s.RefreshStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i, changes := range []int{1, 0} {
		v := stats[i]
		if v.Checks != 1 || v.Changes != changes || v.CheckSeconds < 23*86400 || time.Since(v.SyncedAt) > time.Minute {
			t.Fatalf("stats: %+v", v)
		}
	}
}
