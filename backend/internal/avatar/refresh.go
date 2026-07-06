package avatar

import (
	"context"
	"database/sql"
	"math/rand"
	"sync"
	"time"
)

type RefreshResult struct {
	Found    int
	NotFound int
	Errors   int
	Skipped  int
}

type RefreshOptions struct {
	Concurrency int
	Delay       time.Duration
}

func RefreshAll(ctx context.Context, db *sql.DB, fetcher *Fetcher, opts RefreshOptions) (RefreshResult, error) {
	known, err := KnownHashes(ctx, db)
	if err != nil {
		return RefreshResult{}, err
	}
	if fetcher == nil {
		fetcher = DefaultFetcher()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	delay := opts.Delay
	if delay == 0 {
		delay = 50 * time.Millisecond
	}

	jobs := make(chan KnownAvatar)
	var mu sync.Mutex
	var result RefreshResult
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobs {
				if delay > 0 {
					jitter := time.Duration(rand.Int63n(int64(delay)))
					timer := time.NewTimer(delay + jitter)
					select {
					case <-ctx.Done():
						timer.Stop()
						mu.Lock()
						result.Errors++
						mu.Unlock()
						continue
					case <-timer.C:
					}
				}
				fetched, err := fetcher.Fetch(ctx, item.EmailHash)
				if err != nil {
					mu.Lock()
					result.Errors++
					mu.Unlock()
					continue
				}
				row := Row{
					EmailHash:    item.EmailHash,
					Status:       fetched.Status,
					Image:        fetched.Image,
					MimeType:     fetched.MimeType,
					ByteSize:     fetched.ByteSize,
					UpstreamETag: fetched.UpstreamETag,
				}
				if err := Put(ctx, db, row); err != nil {
					mu.Lock()
					result.Errors++
					mu.Unlock()
					continue
				}
				mu.Lock()
				if fetched.Status == StatusFound {
					result.Found++
				} else {
					result.NotFound++
				}
				mu.Unlock()
			}
		}()
	}
	for _, item := range known {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return result, ctx.Err()
		case jobs <- item:
		}
	}
	close(jobs)
	wg.Wait()
	return result, nil
}
