// Copyright 2013 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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

//zhaopeng-iri 删掉了所有的获取target并开启scrap操作的动作,只获取scrub

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
			sp.targets[hash] = t
		}
	}

	// Stop and remove old targets and scraper loops.
	for hash := range sp.targets {
		if _, ok := uniqueTargets[hash]; !ok {
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

	sp.mtx.Lock()
	defer sp.mtx.Unlock()

	client, err := httputil.NewClientFromConfig(cfg.HTTPClientConfig)
	if err != nil {
		// Any errors that could occur here should be caught during config validation.
		log.Errorf("Error creating HTTP client for job %q: %s", cfg.JobName, err)
	}
	sp.config = cfg
	sp.client = client

}
