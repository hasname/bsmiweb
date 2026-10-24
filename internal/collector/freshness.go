package collector

import (
	"cmp"
	"math"
	"slices"
	"time"

	"github.com/hasname/bsmiweb/internal/db"
)

// Refresh scheduling follows the freshness-optimal resource allocation of
// Cho and Garcia-Molina, "Synchronizing a database to improve freshness"
// (SIGMOD 2000), with the fixed-order synchronization policy. Change rates use
// the estimator from Cho and Garcia-Molina, "Estimating frequency of change"
// (ACM TOIT 2003). Rates and frequencies are per day.

// defaultChangeRate is used only before any registration has been checked.
// When every rate is equal, the optimal allocation is uniform for any value.
const defaultChangeRate = 1.0 / 30

// changeRate estimates λ from n checks that detected x changes over the given
// total number of days, using −log((n−X+0.5)/(n+0.5)) per average interval.
// Unlike the paper, half a check with a change is added, so registrations
// without detected changes keep a positive rate and are still revisited.
func changeRate(n, x int, days float64) float64 {
	if n <= 0 || days <= 0 {
		return 0
	}
	return -math.Log((float64(n-x)+0.5)/(float64(n)+1)) * float64(n) / days
}

// freshnessSlope is ∂F̄(λ, f)/∂f, where F̄(λ, f) = (1−e^(−λ/f))/(λ/f) is the
// time-averaged freshness of an element changing at rate λ and synchronized
// at a fixed interval 1/f.
func freshnessSlope(rate, freq float64) float64 {
	r := rate / freq
	return -math.Expm1(-r)/rate - math.Exp(-r)/freq
}

// frequency solves ∂F̄(λ, f)/∂f = μ for f within [0, limit]. The slope decreases
// from 1/λ toward 0 as f grows, so elements with λ ≥ 1/μ, which change too
// often to be kept fresh, are not synchronized at all.
func frequency(rate, mu, limit float64) float64 {
	if rate*mu >= 1 {
		return 0
	}
	if freshnessSlope(rate, limit) >= mu {
		return limit
	}
	lo, hi := 0.0, limit
	for range 50 {
		mid := (lo + hi) / 2
		if freshnessSlope(rate, mid) > mu {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// allocate maximizes Σ F̄(λᵢ, fᵢ) subject to Σ fᵢ = budget and 0 ≤ fᵢ ≤ limit.
// By the method of Lagrange multipliers, every fᵢ satisfies
// ∂F̄(λᵢ, fᵢ)/∂fᵢ = μ, and μ is found by bisection. Rates must be positive.
func allocate(rates []float64, budget, limit float64) []float64 {
	freqs := make([]float64, len(rates))
	if budget >= float64(len(rates))*limit {
		for i := range freqs {
			freqs[i] = limit
		}
		return freqs
	}
	total := func(mu float64) float64 {
		sum := 0.0
		for i, rate := range rates {
			freqs[i] = frequency(rate, mu, limit)
			sum += freqs[i]
		}
		return sum
	}
	// The total decreases as μ grows and is zero once μ ≥ 1/min(λ).
	lo, hi := 0.0, 1/slices.Min(rates)
	for range 50 {
		mid := (lo + hi) / 2
		if total(mid) > budget {
			lo = mid
		} else {
			hi = mid
		}
	}
	total(hi)
	return freqs
}

// refreshOrder chooses up to budget registrations to check in a daily run.
// Each registration is due after 1/f days, so the most overdue relative to its
// optimal frequency comes first. Registrations without checks use the rate
// estimated from all checks.
func refreshOrder(stats []db.RefreshStat, now time.Time, budget int) []db.RefreshStat {
	if len(stats) == 0 || budget <= 0 {
		return nil
	}
	checks, changes, seconds := 0, 0, int64(0)
	for _, s := range stats {
		checks += s.Checks
		changes += s.Changes
		seconds += s.CheckSeconds
	}
	pooled := changeRate(checks, changes, float64(seconds)/86400)
	if pooled == 0 {
		pooled = defaultChangeRate
	}
	rates := make([]float64, len(stats))
	for i, s := range stats {
		rates[i] = changeRate(s.Checks, s.Changes, float64(s.CheckSeconds)/86400)
		if rates[i] == 0 {
			rates[i] = pooled
		}
	}
	// The daily schedule cannot check a registration more than once a day.
	freqs := allocate(rates, float64(budget), 1)
	type candidate struct {
		stat     db.RefreshStat
		priority float64
	}
	var candidates []candidate
	for i, s := range stats {
		if freqs[i] > 0 {
			candidates = append(candidates, candidate{s, now.Sub(s.SyncedAt).Hours() / 24 * freqs[i]})
		}
	}
	slices.SortStableFunc(candidates, func(a, b candidate) int { return cmp.Compare(b.priority, a.priority) })
	order := make([]db.RefreshStat, 0, min(budget, len(candidates)))
	for _, c := range candidates[:min(budget, len(candidates))] {
		order = append(order, c.stat)
	}
	return order
}
