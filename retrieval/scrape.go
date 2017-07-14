package retrieval

import (
	"context"
	"net/http"
	"sync"

	"github.com/prometheus/common/log"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/httputil"
)

type scrapePool struct {
	appender storage.SampleAppender
	ctx      context.Context

	mtx    sync.RWMutex
	config *config.ScrapeConfig
	client *http.Client
	// Targets and loops must always be synchronized to have the same
	// set of hashes.
	targets map[uint64]*Target
}

func (sp *scrapePool) Sync(tgs []*config.TargetGroup) {
	//start := time.Now()

	var all []*Target
	for _, tg := range tgs {
		targets, err := targetsFromGroup(tg, sp.config)
		if err != nil {
			log.With("err", err).Error("creating targets failed")
			continue
		}
		all = append(all, targets...)
	}
	sp.sync(all)

}
func newScrapePool(ctx context.Context, cfg *config.ScrapeConfig, app storage.SampleAppender) *scrapePool {
	/*
		client, err := httputil.NewClientFromConfig(cfg.HTTPClientConfig)
		if err != nil {
			// Any errors that could occur here should be caught during config validation.
			log.Errorf("Error creating HTTP client for job %q: %s", cfg.JobName, err)
		}
	*/
	return &scrapePool{
		appender: app,
		config:   cfg,
		ctx:      ctx,
		targets:  map[uint64]*Target{},
	}
}

// sync takes a list of potentially duplicated targets, deduplicates them, starts
// scrape loops for new targets, and stops scrape loops for disappeared targets.
// It returns after all stopped scrape loops terminated.
func (sp *scrapePool) sync(targets []*Target) {
	sp.mtx.Lock()
	defer sp.mtx.Unlock()

	var (
		uniqueTargets = map[uint64]struct{}{}
		//interval      = time.Duration(sp.config.ScrapeInterval)
		//timeout       = time.Duration(sp.config.ScrapeTimeout)
	)

	for _, t := range targets {
		hash := t.hash()
		uniqueTargets[hash] = struct{}{}

		if _, ok := sp.targets[hash]; !ok {
			/*
				s := &targetScraper{
					Target:  t,
					client:  sp.client,
					timeout: timeout,
				}

				l := sp.newLoop(sp.ctx, s, sp.appender, t.Labels(), sp.config)
			*/

			sp.targets[hash] = t
			//sp.loops[hash] = l

			//go l.run(interval, timeout, nil)
		}
	}

	//var wg sync.WaitGroup

	// Stop and remove old targets and scraper loops.
	for hash := range sp.targets {
		if _, ok := uniqueTargets[hash]; !ok {
			//wg.Add(1)
			/*
				go func(l loop) {
					l.stop()
					wg.Done()
				}(sp.loops[hash])

				delete(sp.loops, hash)
			*/
			delete(sp.targets, hash)
		}
	}

	// Wait for all potentially stopped scrapers to terminate.
	// This covers the case of flapping targets. If the server is under high load, a new scraper
	// may be active and tries to insert. The old scraper that didn't terminate yet could still
	// be inserting a previous sample set.
	//wg.Wait()
}

// reload the scrape pool with the given scrape configuration. The target state is preserved
// but all scrape loops are restarted with the new scrape configuration.
// This method returns after all scrape loops that were stopped have fully terminated.
func (sp *scrapePool) reload(cfg *config.ScrapeConfig) {
	//start := time.Now()

	sp.mtx.Lock()
	defer sp.mtx.Unlock()

	client, err := httputil.NewClientFromConfig(cfg.HTTPClientConfig)
	if err != nil {
		// Any errors that could occur here should be caught during config validation.
		log.Errorf("Error creating HTTP client for job %q: %s", cfg.JobName, err)
	}
	sp.config = cfg
	sp.client = client

	//var (
	//wg sync.WaitGroup
	//interval = time.Duration(sp.config.ScrapeInterval)
	//timeout  = time.Duration(sp.config.ScrapeTimeout)
	//)

	/*
		for fp, oldLoop := range sp.loops {
			var (
				t = sp.targets[fp]
				s = &targetScraper{
					Target:  t,
					client:  sp.client,
					timeout: timeout,
				}
				newLoop = sp.newLoop(sp.ctx, s, sp.appender, t.Labels(), sp.config)
			)
			wg.Add(1)

			go func(oldLoop, newLoop loop) {
				oldLoop.stop()
				wg.Done()

				go newLoop.run(interval, timeout, nil)
			}(oldLoop, newLoop)

			sp.loops[fp] = newLoop
		}
	*/

	//wg.Wait()
	/*
		targetReloadIntervalLength.WithLabelValues(interval.String()).Observe(
			time.Since(start).Seconds(),
		)
	*/
}
