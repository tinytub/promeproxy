package v1

// API can register a set of endpoints in a router and handle
import (
	"context"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage/metric"
	"github.com/prometheus/prometheus/util/httputil"
	"github.com/tinytub/promeproxy/retrieval"
	"github.com/tinytub/promeproxy/utils"
)

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

var corsHeaders = map[string]string{
	"Access-Control-Allow-Headers":  "Accept, Authorization, Content-Type, Origin",
	"Access-Control-Allow-Methods":  "GET, OPTIONS",
	"Access-Control-Allow-Origin":   "*",
	"Access-Control-Expose-Headers": "Date",
}

type apiError struct {
	typ errorType
	err error
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s: %s", e.typ, e.err)
}

type targetRetriever interface {
	Targets() []*retrieval.Target
}

type response struct {
	Status    status      `json:"status"`
	Data      interface{} `json:"data,omitempty"`
	ErrorType errorType   `json:"errorType,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// Enables cross-site script calls.
func setCORS(w http.ResponseWriter) {
	for h, v := range corsHeaders {
		w.Header().Set(h, v)
	}
}

type apiFunc func(r *http.Request) (interface{}, *apiError)

// them using the provided storage and query engine.
type API struct {
	targetRetriever targetRetriever

	now func() model.Time
}

// NewAPI returns an initialized API type.
func NewAPI(tr targetRetriever) *API {
	return &API{
		targetRetriever: tr,
		now:             model.Now,
	}
}

// Register the API's endpoints in the given router.
func (api *API) Register(r *route.Router) {
	instr := func(name string, f apiFunc) http.HandlerFunc {
		hf := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setCORS(w)
			if data, err := f(r); err != nil {
				respondError(w, err, data)
			} else if data != nil {
				respond(w, data)
			} else {
				w.WriteHeader(http.StatusNoContent)
			}
		})
		return prometheus.InstrumentHandler(name, httputil.CompressionHandler{
			Handler: hf,
		})
	}

	r.Options("/*path", instr("options", api.options))

	r.Get("/query", instr("query", api.query))
	r.Get("/query_range", instr("query_range", api.queryRange))
	log.Info("route registed")
	/*

		r.Get("/label/:name/values", instr("label_values", api.labelValues))

		r.Get("/series", instr("series", api.series))
		r.Del("/series", instr("drop_series", api.dropSeries))

		r.Get("/targets", instr("targets", api.targets))
		r.Get("/alertmanagers", instr("alertmanagers", api.alertmanagers))
	*/
}

type queryData struct {
	ResultType model.ValueType `json:"resultType"`
	Result     model.Value     `json:"result"`
}

func (api *API) options(r *http.Request) (interface{}, *apiError) {
	return nil, nil
}

func (api *API) query(r *http.Request) (interface{}, *apiError) {
	var ts model.Time
	if t := r.FormValue("time"); t != "" {
		var err error
		ts, err = parseTime(t)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
	} else {
		ts = api.now()
	}

	ctx := r.Context()
	if to := r.FormValue("timeout"); to != "" {
		var cancel context.CancelFunc
		timeout, err := parseDuration(to)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}

		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	log.Info(r.FormValue("query"), ts)

	/*
			qry, err := api.QueryEngine.NewInstantQuery(r.FormValue("query"), ts)

			if err != nil {
				return nil, &apiError{errorBadData, err}
			}

			res := qry.Exec(ctx)
			if res.Err != nil {
				switch res.Err.(type) {
				case promql.ErrQueryCanceled:
					return nil, &apiError{errorCanceled, res.Err}
				case promql.ErrQueryTimeout:
					return nil, &apiError{errorTimeout, res.Err}
				case promql.ErrStorage:
					return nil, &apiError{errorInternal, res.Err}
				}
				return nil, &apiError{errorExec, res.Err}
			}
		return &queryData{
			ResultType: res.Value.Type(),
			Result:     res.Value,
		}, nil
	*/
	return &queryData{
		ResultType: 0,
		Result:     nil,
	}, nil
}

func (api *API) queryRange(r *http.Request) (interface{}, *apiError) {
	start, err := parseTime(r.FormValue("start"))
	if err != nil {
		return nil, &apiError{errorBadData, err}
	}
	end, err := parseTime(r.FormValue("end"))
	if err != nil {
		return nil, &apiError{errorBadData, err}
	}
	if end.Before(start) {
		err := errors.New("end timestamp must not be before start time")
		return nil, &apiError{errorBadData, err}
	}

	step, err := parseDuration(r.FormValue("step"))
	if err != nil {
		return nil, &apiError{errorBadData, err}
	}

	if step <= 0 {
		err := errors.New("zero or negative query resolution step widths are not accepted. Try a positive integer")
		return nil, &apiError{errorBadData, err}
	}

	// For safety, limit the number of returned points per timeseries.
	// This is sufficient for 60s resolution for a week or 1h resolution for a year.
	if end.Sub(start)/step > 11000 {
		err := errors.New("exceeded maximum resolution of 11,000 points per timeseries. Try decreasing the query resolution (?step=XX)")
		return nil, &apiError{errorBadData, err}
	}

	ctx := r.Context()
	if to := r.FormValue("timeout"); to != "" {
		var cancel context.CancelFunc
		timeout, err := parseDuration(to)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}

		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	/*
		expr, err := promql.ParseExpr(r.FormValue("query"))
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
		if expr.Type() != model.ValVector && expr.Type() != model.ValScalar {
			return nil, &apiError{errorBadData, fmt.Errorf("invalid expression type %q for range query, must be scalar or instant vector", documentedType(expr.Type()))}
		}

		thash := api.getTargets()
		//target与prometheus服务器的分配
		es := &promql.EvalStmt{
			Expr:     expr,
			Start:    start,
			End:      end,
			Interval: time.Duration(60 * time.Second),
		}

		matchers, err := populateIterators(ctx, es)
		for _, v := range matchers {
			for _, n := range thash.RawTargets {
				if v.Match(n.Address) {
					fmt.Printf("match!! mod: %d, address: %s\n", n.Mod, n.Address)
					//TODO 从表里查 mod:remoteprometheus对应关系
					//读出来，插入到新的list中作为需要生成的客户端列表留用
				}
			}
		}
	*/
	promtargets := api.getPromTargets()
	//var finalres *queryRes
	//var finalres model.Matrix

	//	sampleStreams := map[model.Fingerprint]*sampleStream{}
	mat := matrix{}
	//wait group 并发执行吧

	for _, t := range promtargets {

		url := "http://" + string(t) + r.RequestURI
		client, _ := utils.NewClientForTimeOut()
		request, err := http.NewRequest("GET", url, strings.NewReader(""))

		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
		resp, err := client.Do(request)
		if resp != nil && resp.Body != nil {
			defer func() {
				io.Copy(ioutil.Discard, resp.Body)
				resp.Body.Close()
			}()
		}
		var res *queryRes

		body, err := ioutil.ReadAll(resp.Body)
		err = json.Unmarshal(body, &res)
		//finalres.Data.Result = append(finalres.Data.Result, res.Data.Result...)
		//finalres = append(finalres, res.Data.Matrix...)
		//fmt.Println(res.Data.matrix)
		/*
			for _, v := range res.Data.Result {
				for k1, v1 := range v.Metric {
					fmt.Println(k1)
					fmt.Println(v1)
				}
			}
		*/
		mat = append(mat, res.Data.Result...)
	}

	final := mat.value()

	return &queryData{
		ResultType: final.Type(),
		Result:     final,
	}, nil
}

/*
func mergeQueryData(is ...int) {
	for i := 0; i < len(is); i++ {
		fmt.Println(is[i])
	}
}
*/

//取query和queryRange中的label
func populateIterators(ctx context.Context, s *promql.EvalStmt) (metric.LabelMatchers, error) {
	var matchers metric.LabelMatchers
	promql.Inspect(s.Expr, func(node promql.Node) bool {
		switch n := node.(type) {
		case *promql.VectorSelector:
			log.Info("populate vectorselector")
			matchers = n.LabelMatchers

		case *promql.MatrixSelector:
			log.Info("populate metrixSelector")
			matchers = n.LabelMatchers
		}
		return true
	})
	return matchers, fmt.Errorf("no Matcheres found")
}

/*
func (api *API) labelValues(r *http.Request) (interface{}, *apiError) {
	name := route.Param(r.Context(), "name")

	if !model.LabelNameRE.MatchString(name) {
		return nil, &apiError{errorBadData, fmt.Errorf("invalid label name: %q", name)}
	}
	q, err := api.Storage.Querier()
	if err != nil {
		return nil, &apiError{errorExec, err}
	}
	defer q.Close()

	vals, err := q.LabelValuesForLabelName(r.Context(), model.LabelName(name))
	if err != nil {
		return nil, &apiError{errorExec, err}
	}
	sort.Sort(vals)

	return vals, nil
}

func (api *API) series(r *http.Request) (interface{}, *apiError) {
	r.ParseForm()
	if len(r.Form["match[]"]) == 0 {
		return nil, &apiError{errorBadData, fmt.Errorf("no match[] parameter provided")}
	}

	var start model.Time
	if t := r.FormValue("start"); t != "" {
		var err error
		start, err = parseTime(t)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
	} else {
		start = model.Earliest
	}

	var end model.Time
	if t := r.FormValue("end"); t != "" {
		var err error
		end, err = parseTime(t)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
	} else {
		end = model.Latest
	}

	var matcherSets []metric.LabelMatchers
	for _, s := range r.Form["match[]"] {
		matchers, err := promql.ParseMetricSelector(s)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
		matcherSets = append(matcherSets, matchers)
	}

	q, err := api.Storage.Querier()
	if err != nil {
		return nil, &apiError{errorExec, err}
	}
	defer q.Close()

	res, err := q.MetricsForLabelMatchers(r.Context(), start, end, matcherSets...)
	if err != nil {
		return nil, &apiError{errorExec, err}
	}

	metrics := make([]model.Metric, 0, len(res))
	for _, met := range res {
		metrics = append(metrics, met.Metric)
	}
	return metrics, nil
}

func (api *API) dropSeries(r *http.Request) (interface{}, *apiError) {
	r.ParseForm()
	if len(r.Form["match[]"]) == 0 {
		return nil, &apiError{errorBadData, fmt.Errorf("no match[] parameter provided")}
	}

	numDeleted := 0
	for _, s := range r.Form["match[]"] {
		matchers, err := promql.ParseMetricSelector(s)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
		n, err := api.Storage.DropMetricsForLabelMatchers(context.TODO(), matchers...)
		if err != nil {
			return nil, &apiError{errorExec, err}
		}
		numDeleted += n
	}

	res := struct {
		NumDeleted int `json:"numDeleted"`
	}{
		NumDeleted: numDeleted,
	}
	return res, nil
}

// Target has the information for one target.
type Target struct {
	// Labels before any processing.
	DiscoveredLabels model.LabelSet `json:"discoveredLabels"`
	// Any labels that are added to this target and its metrics.
	Labels model.LabelSet `json:"labels"`

	ScrapeURL string `json:"scrapeUrl"`

	LastError  string                 `json:"lastError"`
	LastScrape time.Time              `json:"lastScrape"`
	Health     retrieval.TargetHealth `json:"health"`
}

// TargetDiscovery has all the active targets.
type TargetDiscovery struct {
	ActiveTargets []*Target `json:"activeTargets"`
}

func (api *API) targets(r *http.Request) (interface{}, *apiError) {
	targets := api.targetRetriever.Targets()
	res := &TargetDiscovery{ActiveTargets: make([]*Target, len(targets))}

	for i, t := range targets {
		lastErrStr := ""
		lastErr := t.LastError()
		if lastErr != nil {
			lastErrStr = lastErr.Error()
		}

		res.ActiveTargets[i] = &Target{
			DiscoveredLabels: t.DiscoveredLabels(),
			Labels:           t.Labels(),
			ScrapeURL:        t.URL().String(),
			LastError:        lastErrStr,
			LastScrape:       t.LastScrape(),
			Health:           t.Health(),
		}
	}

	return res, nil
}

// AlertmanagerDiscovery has all the active Alertmanagers.
type AlertmanagerDiscovery struct {
	ActiveAlertmanagers []*AlertmanagerTarget `json:"activeAlertmanagers"`
}

// AlertmanagerTarget has info on one AM.
type AlertmanagerTarget struct {
	URL string `json:"url"`
}

func (api *API) alertmanagers(r *http.Request) (interface{}, *apiError) {
	urls := api.alertmanagerRetriever.Alertmanagers()
	ams := &AlertmanagerDiscovery{ActiveAlertmanagers: make([]*AlertmanagerTarget, len(urls))}

	for i, url := range urls {
		ams.ActiveAlertmanagers[i] = &AlertmanagerTarget{URL: url.String()}
	}

	return ams, nil
}
*/

type targetHash struct {
	RawTargets []rawTarget
}

type rawTarget struct {
	Mod     uint64            `json:"mod"`
	Target  *retrieval.Target `json:"target"`
	Address model.LabelValue  `json:"address"`
}

func (api *API) getTargets() targetHash {
	targets := api.targetRetriever.Targets()
	//TODO models 从配置里取，这里暂时写死 3
	var thash targetHash
	models := uint64(3)
	for _, v := range targets {
		mod := sum64(md5.Sum([]byte(v.Labels()["instance"]))) % models
		rt := &rawTarget{Mod: mod, Target: v, Address: v.DiscoveredLabels()["__address__"]}
		thash.RawTargets = append(thash.RawTargets, *rt)
	}
	return thash
}

func (api *API) getPromTargets() []model.LabelValue {
	targets := api.targetRetriever.Targets()
	var tl []model.LabelValue
	for _, v := range targets {
		if v.Labels()["job"] == "allprometheus" {
			tl = append(tl, v.Labels()["instance"])
		}
	}
	return tl
}

func respond(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	b, err := json.Marshal(&response{
		Status: statusSuccess,
		Data:   data,
	})
	if err != nil {
		return
	}
	w.Write(b)
}

func respondError(w http.ResponseWriter, apiErr *apiError, data interface{}) {
	w.Header().Set("Content-Type", "application/json")

	var code int
	switch apiErr.typ {
	case errorBadData:
		code = http.StatusBadRequest
	case errorExec:
		code = 422
	case errorCanceled, errorTimeout:
		code = http.StatusServiceUnavailable
	case errorInternal:
		code = http.StatusInternalServerError
	default:
		code = http.StatusInternalServerError
	}
	w.WriteHeader(code)

	b, err := json.Marshal(&response{
		Status:    statusError,
		ErrorType: apiErr.typ,
		Error:     apiErr.err.Error(),
		Data:      data,
	})
	if err != nil {
		return
	}
	w.Write(b)
}

func parseTime(s string) (model.Time, error) {
	if t, err := strconv.ParseFloat(s, 64); err == nil {
		ts := t * float64(time.Second)
		if ts > float64(math.MaxInt64) || ts < float64(math.MinInt64) {
			return 0, fmt.Errorf("cannot parse %q to a valid timestamp. It overflows int64", s)
		}
		return model.TimeFromUnixNano(int64(ts)), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return model.TimeFromUnixNano(t.UnixNano()), nil
	}
	return 0, fmt.Errorf("cannot parse %q to a valid timestamp", s)
}

func parseDuration(s string) (time.Duration, error) {
	if d, err := strconv.ParseFloat(s, 64); err == nil {
		ts := d * float64(time.Second)
		if ts > float64(math.MaxInt64) || ts < float64(math.MinInt64) {
			return 0, fmt.Errorf("cannot parse %q to a valid duration. It overflows int64", s)
		}
		return time.Duration(ts), nil
	}
	if d, err := model.ParseDuration(s); err == nil {
		return time.Duration(d), nil
	}
	return 0, fmt.Errorf("cannot parse %q to a valid duration", s)
}

func sum64(hash [md5.Size]byte) uint64 {
	var s uint64

	for i, b := range hash {
		shift := uint64((md5.Size - i - 1) * 8)

		s |= uint64(b) << shift
	}
	return s
}

// documentedType returns the internal type to the equivalent
// user facing terminology as defined in the documentation.
func documentedType(t model.ValueType) string {
	switch t.String() {
	case "vector":
		return "instant vector"
	case "matrix":
		return "range vector"
	default:
		return t.String()
	}
}
