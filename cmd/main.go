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

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/tinytub/promeproxy/retrieval"
	"github.com/tinytub/promeproxy/storage"
	"github.com/tinytub/promeproxy/storage/fanin"
	"github.com/tinytub/promeproxy/storage/remote"
	"github.com/tinytub/promeproxy/web"
)

func main() {
	os.Exit(Main())
}

type status string

const (
	statusSuccess status = "success"
	statusError          = "error"
)

type errorType string

const (
	errorNone     errorType = ""
	errorTimeout            = "timeout"
	errorCanceled           = "canceled"
	errorExec               = "execution"
	errorBadData            = "bad_data"
	errorInternal           = "internal"
)

type response struct {
	Status    status      `json:"status"`
	Data      interface{} `json:"data,omitempty"`
	ErrorType errorType   `json:"errorType,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type queryData struct {
	ResultType model.ValueType `json:"resultType"`
	Result     model.Value     `json:"result"`
}

type mockSyncer struct {
	sync func(tgs []*config.TargetGroup)
}

func (s *mockSyncer) Sync(tgs []*config.TargetGroup) {
	if s.sync != nil {
		s.sync(tgs)
	}
}

func Main() int {
	if err := parse(os.Args[1:]); err != nil {
		log.Error(err)
		return 2
	}
	if cfg.printVersion {
		fmt.Fprintln(os.Stdout, version.Print("prometheus"))
		return 0
	}
	var (
		sampleAppender = storage.Fanout{}
		reloadables    []Reloadable
	)
	var localStorage local.Storage
	localStorage = &local.NoopStorage{}
	remoteAppender := &remote.Writer{}
	remoteReader := &remote.Reader{}
	sampleAppender = append(sampleAppender, remoteAppender)

	reloadables = append(reloadables, remoteAppender, remoteReader)

	queryable := fanin.Queryable{
		Local:  localStorage,
		Remote: remoteReader,
	}
	var (
		targetManager  = retrieval.NewTargetManager(sampleAppender, log.Base())
		ctx, cancelCtx = context.WithCancel(context.Background())
		queryEngine    = promql.NewEngine(queryable, &cfg.queryEngine)
	)

	cfg.web.QueryEngine = queryEngine
	cfg.web.TargetManager = targetManager
	cfg.web.Context = ctx
	cfg.web.Flags = map[string]string{}
	cfg.fs.VisitAll(func(f *flag.Flag) {
		cfg.web.Flags[f.Name] = f.Value.String()
	})

	webHandler := web.New(&cfg.web)

	reloadables = append(reloadables, targetManager, webHandler)
	if err := reloadConfig(cfg.configFile, reloadables...); err != nil {
		log.Errorf("Error loading config: %s", err)
		return 1
	}

	hup := make(chan os.Signal)
	hupReady := make(chan bool)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		<-hupReady
		for {
			select {
			case <-hup:
				if err := reloadConfig("./prometheus.yml", reloadables...); err != nil {
					log.Errorf("Error reloading config: %s", err)
				}
				log.Info("got hup")

			}
		}
	}()

	go targetManager.Run()
	defer targetManager.Stop()

	defer cancelCtx()
	go webHandler.Run()

	close(hupReady)

	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	select {
	case <-term:
		log.Warn("Received SIGTERM, exiting gracefully...")
	case <-webHandler.Quit():
		log.Warn("Received termination request via web service, exiting gracefully...")
	case err := <-webHandler.ListenError():
		log.Errorln("Error starting web server, exiting gracefully:", err)
	}
	return 0
}

type Reloadable interface {
	ApplyConfig(*config.Config) error
}

func reloadConfig(filename string, rls ...Reloadable) (err error) {
	log.Infof("Loading configuration file %s", filename)

	conf, err := config.LoadFile(filename)
	if err != nil {
		return fmt.Errorf("couldn't load configuration (-config.file=%s): %v", filename, err)
	}

	failed := false
	for _, rl := range rls {
		if err := rl.ApplyConfig(conf); err != nil {
			log.Error("Failed to apply configuration: ", err)
			failed = true
		}
	}
	if failed {
		return fmt.Errorf("one or more errors occurred while applying the new configuration (-config.file=%s)", filename)
	}
	return nil
}
