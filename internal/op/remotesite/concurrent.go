package remotesite

import (
	"context"
	"runtime"

	"golang.org/x/sync/errgroup"
)

// maxRemoteSiteConcurrency bounds how many remote sites are processed in
// parallel by the scheduled batch jobs (balance capture, check-in,
// announcement fetch, usage sync, refresh). Each per-site operation issues a
// network request to the upstream relay site with up to a 30s timeout, so
// running them serially makes a single batch take O(N × 30s) in the worst
// case when many sites are slow or unreachable. A bounded pool keeps the
// wall-clock time near that of the slowest single site without opening an
// unbounded number of concurrent connections.
func maxRemoteSiteConcurrency() int {
	if n := runtime.GOMAXPROCS(0) * 2; n > 8 {
		return n
	}
	return 8
}

// forEachSiteConcurrent runs fn for every site ID concurrently with a bounded
// worker pool. fn must be safe for concurrent use; callers that accumulate
// results must guard shared state themselves. The provided context is passed
// through to each invocation. fn's error is ignored here (callers log inside
// fn as the serial versions did); the helper only coordinates concurrency.
func forEachSiteConcurrent(ctx context.Context, siteIDs []int, fn func(ctx context.Context, siteID int)) {
	if len(siteIDs) == 0 {
		return
	}
	if len(siteIDs) == 1 {
		fn(ctx, siteIDs[0])
		return
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxRemoteSiteConcurrency())
	for _, id := range siteIDs {
		id := id
		g.Go(func() error {
			fn(gctx, id)
			return nil
		})
	}
	_ = g.Wait()
}
