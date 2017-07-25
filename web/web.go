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

package web

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sync"
	"time"

	"github.com/opentracing-contrib/go-stdlib/nethttp"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"
	"golang.org/x/net/context"
	"golang.org/x/net/netutil"

	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/tinytub/promeproxy/retrieval"
	api_v1 "github.com/tinytub/promeproxy/web/api/v1"
)

var localhostRepresentations = []string{"127.0.0.1", "localhost"}

// Handler serves various HTTP endpoints of the Prometheus server
type Handler struct {
	targetManager *retrieval.TargetManager
	queryEngine   *promql.Engine
	context       context.Context
	/*
		ruleManager   *rules.Manager
		queryEngine   *promql.Engine
		storage       local.Storage
		notifier      *notifier.Notifier
	*/

	apiV1 *api_v1.API

	router      *route.Router
	listenErrCh chan error
	options     *Options
	quitCh      chan struct{}
	reloadCh    chan chan error
	birth       time.Time
	cwd         string
	flagsMap    map[string]string

	configString string

	externalLabels model.LabelSet
	mtx            sync.RWMutex
	now            func() model.Time
}

// ApplyConfig updates the status state as the new config requires.
func (h *Handler) ApplyConfig(conf *config.Config) error {
	h.mtx.Lock()
	defer h.mtx.Unlock()

	h.externalLabels = conf.GlobalConfig.ExternalLabels
	h.configString = conf.String()

	return nil
}

/*
// PrometheusVersion contains build information about Prometheus.
type PrometheusVersion struct {
	Version   string `json:"version"`
	Revision  string `json:"revision"`
	Branch    string `json:"branch"`
	BuildUser string `json:"buildUser"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}
*/

// Options for the web Handler.
type Options struct {
	Context     context.Context
	QueryEngine *promql.Engine
	/*
		Storage       local.Storage
		QueryEngine   *promql.Engine
		RuleManager   *rules.Manager
		Notifier      *notifier.Notifier
	*/
	TargetManager *retrieval.TargetManager
	Flags         map[string]string

	ListenAddress        string
	ReadTimeout          time.Duration
	MaxConnections       int
	ExternalURL          *url.URL
	RoutePrefix          string
	MetricsPath          string
	UseLocalAssets       bool
	UserAssetsPath       string
	ConsoleTemplatesPath string
	ConsoleLibrariesPath string
	EnableQuit           bool
}

// New initializes a new web Handler.
func New(o *Options) *Handler {
	router := route.New()

	cwd, err := os.Getwd()

	if err != nil {
		cwd = "<error retrieving current working directory>"
	}

	h := &Handler{
		router:      router,
		listenErrCh: make(chan error),
		quitCh:      make(chan struct{}),
		reloadCh:    make(chan chan error),
		options:     o,
		birth:       time.Now(),
		cwd:         cwd,
		flagsMap:    o.Flags,

		context:       o.Context,
		targetManager: o.TargetManager,
		queryEngine:   o.QueryEngine,
		apiV1:         api_v1.NewAPI(o.QueryEngine, o.TargetManager),

		now: model.Now,
	}

	if o.RoutePrefix != "/" {
		// If the prefix is missing for the root path, prepend it.
		router.Get("/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, o.RoutePrefix, http.StatusFound)
		})
		router = router.WithPrefix(o.RoutePrefix)
	}

	//instrh := prometheus.InstrumentHandler
	//instrf := prometheus.InstrumentHandlerFunc

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, path.Join(o.ExternalURL.Path, "/graph"), http.StatusFound)
	})

	/*
		router.Get("/alerts", instrf("alerts", h.alerts))
		router.Get("/graph", instrf("graph", h.graph))
		router.Get("/status", instrf("status", h.status))
		router.Get("/flags", instrf("flags", h.flags))
		router.Get("/config", instrf("config", h.config))
		router.Get("/rules", instrf("rules", h.rules))
		router.Get("/targets", instrf("targets", h.targets))
		router.Get("/version", instrf("version", h.version))

		router.Get("/heap", instrf("heap", dumpHeap))

		router.Get(o.MetricsPath, prometheus.Handler().ServeHTTP)

		router.Get("/federate", instrh("federate", httputil.CompressionHandler{
			Handler: http.HandlerFunc(h.federation),
		}))
	*/

	h.apiV1.Register(router.WithPrefix("/api/v1"))

	/*
			router.Get("/consoles/*filepath", instrf("consoles", h.consoles))

			router.Get("/static/*filepath", instrf("static", serveStaticAsset))

		if o.UserAssetsPath != "" {
			router.Get("/user/*filepath", instrf("user", route.FileServe(o.UserAssetsPath)))
		}
	*/

	if o.EnableQuit {
		router.Post("/-/quit", h.quit)
	}

	router.Post("/-/reload", h.reload)
	router.Get("/-/reload", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprintf(w, "This endpoint requires a POST request.\n")
	})

	/*
		router.Get("/debug/*subpath", http.DefaultServeMux.ServeHTTP)
		router.Post("/debug/*subpath", http.DefaultServeMux.ServeHTTP)
	*/

	return h
}

// ListenError returns the receive-only channel that signals errors while starting the web server.
func (h *Handler) ListenError() <-chan error {
	return h.listenErrCh
}

// Quit returns the receive-only quit channel.
func (h *Handler) Quit() <-chan struct{} {
	return h.quitCh
}

// Reload returns the receive-only channel that signals configuration reload requests.
func (h *Handler) Reload() <-chan chan error {
	return h.reloadCh
}

// Run serves the HTTP endpoints.
func (h *Handler) Run() {
	log.Infof("Listening on %s", h.options.ListenAddress)
	operationName := nethttp.OperationNameFunc(func(r *http.Request) string {
		return fmt.Sprintf("%s %s", r.Method, r.URL.Path)
	})
	server := &http.Server{
		Addr:        h.options.ListenAddress,
		Handler:     nethttp.Middleware(opentracing.GlobalTracer(), h.router, operationName),
		ErrorLog:    log.NewErrorLogger(),
		ReadTimeout: h.options.ReadTimeout,
	}
	listener, err := net.Listen("tcp", h.options.ListenAddress)
	if err != nil {
		log.Error(err)
		h.listenErrCh <- err
	} else {
		log.Info("limitedListener")
		limitedListener := netutil.LimitListener(listener, h.options.MaxConnections)
		h.listenErrCh <- server.Serve(limitedListener)
	}
	log.Info("run ok")
}

func (h *Handler) quit(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Requesting termination... Goodbye!")
	close(h.quitCh)
}

func (h *Handler) reload(w http.ResponseWriter, r *http.Request) {
	rc := make(chan error)
	h.reloadCh <- rc
	if err := <-rc; err != nil {
		http.Error(w, fmt.Sprintf("failed to reload config: %s", err), http.StatusInternalServerError)
	}
}
