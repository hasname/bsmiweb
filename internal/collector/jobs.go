package collector

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/hasname/bsmiweb/internal/db"
)

type Jobs struct {
	Client   *Client
	Store    *db.Store
	Location *time.Location
}

func (j *Jobs) Run(ctx context.Context, name string) error {
	switch name {
	case "import":
		if err := j.Client.ImportCertificates(ctx, j.Store); err != nil {
			return err
		}
		return j.Client.ImportAuthorizations(ctx, j.Store)
	case "import-certificates":
		return j.Client.ImportCertificates(ctx, j.Store)
	case "import-authorizations":
		return j.Client.ImportAuthorizations(ctx, j.Store)
	case "recent":
		return j.recent(ctx)
	case "pchome", "momo", "books", "friday":
		return j.ec(ctx, name)
	default:
		return fmt.Errorf("unknown job %q", name)
	}
}

func report(err error) { log.Print(err); sentry.CaptureException(err) }

// refreshBudget is the number of registrations checked per daily run. At about
// three seconds per check, a run ends before the next scheduled job starts.
const refreshBudget = 1000

// recent checks the registrations chosen for freshness and refreshes those with
// certificates modified since they were last synchronized.
func (j *Jobs) recent(ctx context.Context) error {
	stats, err := j.Store.RefreshStats(ctx)
	if err != nil {
		return err
	}
	var failures []error
	for _, s := range refreshOrder(stats, time.Now(), refreshBudget) {
		now := time.Now()
		changed, err := j.Client.HasRecentChanges(ctx, s.ID, s.SyncedAt.In(j.Location), now.In(j.Location))
		if err == nil && changed {
			err = j.refresh(ctx, s.ID)
		}
		if err == nil {
			err = j.Store.RecordCheck(ctx, s.ID, changed, int64(now.Sub(s.SyncedAt)/time.Second))
		}
		if err != nil {
			if errors.Is(err, ErrRateLimited) || ctx.Err() != nil {
				return err
			}
			report(err)
			failures = append(failures, err)
		}
		if err := j.Client.pause(ctx, 2*time.Second); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

func (j *Jobs) refresh(ctx context.Context, id string) error {
	r, err := j.Client.FetchRegistration(ctx, id)
	if err != nil {
		return err
	}
	if r == nil {
		return nil
	}
	return j.Store.SaveRegistration(ctx, r)
}
func (j *Jobs) ec(ctx context.Context, source string) error {
	pages := 5
	if source == "books" {
		pages = 6
	}
	ids, err := j.Client.Scan(ctx, source, pages)
	if err != nil {
		return err
	}
	var failures []error
	for _, id := range ids {
		r, err := j.Store.Registration(ctx, id)
		if err != nil {
			return err
		}
		if r != nil {
			continue
		}
		if err := j.refresh(ctx, id); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			report(err)
			failures = append(failures, err)
		}
		if err := j.Client.pause(ctx, 2*time.Second); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

var schedule = map[int]string{3: "import", 4: "recent", 5: "pchome", 6: "momo", 7: "books", 8: "friday"}

// Daemon follows the original daily schedule in TZ (or the host's local zone).
// A single running job causes later scheduled jobs to be skipped, not queued.
func (j *Jobs) Daemon(ctx context.Context) error {
	var running atomic.Bool
	done := make(chan struct{}, 1)
	for {
		now := time.Now()
		next := now.Truncate(time.Minute).Add(time.Minute)
		if err := Sleep(ctx, time.Until(next)); err != nil {
			if running.Load() {
				<-done
			}
			return nil
		}
		local := next.In(j.Location)
		if local.Minute() != 0 {
			continue
		}
		name, ok := schedule[local.Hour()]
		if !ok {
			continue
		}
		if !running.CompareAndSwap(false, true) {
			log.Printf("skipping %s: previous job still running", name)
			continue
		}
		// Drain the previous completion notification before starting another job.
		select {
		case <-done:
		default:
		}
		go func() {
			log.Printf("starting %s", name)
			if err := j.Run(ctx, name); err != nil {
				report(err)
			}
			log.Printf("finished %s", name)
			done <- struct{}{}
			running.Store(false)
		}()
	}
}
