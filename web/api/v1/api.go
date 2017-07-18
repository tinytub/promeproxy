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
	"sort"
	"strconv"
	"strings"
	"sync"
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

	r.Get("/label/:name/values", instr("label_values", api.labelValues))

	r.Get("/series", instr("series", api.series))
	r.Del("/series", instr("drop_series", api.dropSeries))

	r.Get("/targets", instr("targets", api.targets))
	r.Get("/alertmanagers", instr("alertmanagers", api.alertmanagers))
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

	mat := matrix{}

	bodys := api.proxyReq(r)

	var wg sync.WaitGroup
	for _, body := range bodys {
		var res *queryRes
		wg.Add(1)
		go func(body []byte) {
			err = json.Unmarshal(body, &res)
			if err != nil {
				fmt.Println(err)
			}

			mat = append(mat, res.Data.Result...)
			wg.Done()
		}(body)
	}
	wg.Wait()

	final := mat.value()

	return &queryData{
		ResultType: final.Type(),
		Result:     final,
	}, nil
}

func (api *API) proxyReq(r *http.Request) [][]byte {
	promtargets := api.getPromTargets()
	var wg sync.WaitGroup
	var bodys [][]byte
	for _, t := range promtargets {
		wg.Add(1)
		go func(r *http.Request, l model.LabelValue) {
			url := "http://" + string(l) + r.RequestURI
			client, _ := utils.NewClientForTimeOut()
			request, _ := http.NewRequest("GET", url, strings.NewReader(""))

			resp, err := client.Do(request)
			if err != nil {
				fmt.Println(err)
			}
			if resp != nil && resp.Body != nil {
				defer func() {
					io.Copy(ioutil.Discard, resp.Body)
					resp.Body.Close()
				}()
			}

			body, err := ioutil.ReadAll(resp.Body)
			bodys = append(bodys, body)
			wg.Done()
		}(r, t)
	}
	wg.Wait()
	return bodys
}

func (api *API) labelValues(r *http.Request) (interface{}, *apiError) {
	name := route.Param(r.Context(), "name")

	if !model.LabelNameRE.MatchString(name) {
		return nil, &apiError{errorBadData, fmt.Errorf("invalid label name: %q", name)}
	}

	bodys := api.proxyReq(r)
	var data model.LabelValues
	for _, body := range bodys {
		var res *labalVals
		err := json.Unmarshal(body, &res)
		if err != nil {
			log.Error(err)
		}
		data = append(data, res.Data...)
	}
	fData := api.removeDuplicate(data)

	sort.Sort(fData)

	return fData, nil
}

func (api *API) series(r *http.Request) (interface{}, *apiError) {
	r.ParseForm()
	if len(r.Form["match[]"]) == 0 {
		return nil, &apiError{errorBadData, fmt.Errorf("no match[] parameter provided")}
	}

	if t := r.FormValue("start"); t != "" {
		var err error
		_, err = parseTime(t)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
	}

	if t := r.FormValue("end"); t != "" {
		var err error
		_, err = parseTime(t)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
	}

	var matcherSets []metric.LabelMatchers
	for _, s := range r.Form["match[]"] {
		matchers, err := promql.ParseMetricSelector(s)
		if err != nil {
			return nil, &apiError{errorBadData, err}
		}
		matcherSets = append(matcherSets, matchers)
	}

	bodys := api.proxyReq(r)
	//metrics := make([]model.Metric, 0, len(res))
	data := make([]seriesVals, 0, len(bodys))
	for _, body := range bodys {
		var res seriesVals
		err := json.Unmarshal(body, &res)
		if err != nil {
			log.Error(err)
		}
		data = append(data, res)
	}

	metrics := make([]model.Metric, 0, len(data))
	for _, d := range data {
		for _, met := range d.Data {
			metrics = append(metrics, met)
		}
	}
	return metrics, nil
}

func (api *API) dropSeries(r *http.Request) (interface{}, *apiError) {
	r.ParseForm()
	if len(r.Form["match[]"]) == 0 {
		return nil, &apiError{errorBadData, fmt.Errorf("no match[] parameter provided")}
	}

	bodys := api.proxyReq(r)
	var numDeleted int
	for _, body := range bodys {
		var res seriesDelVals
		err := json.Unmarshal(body, &res)
		if err != nil {
			log.Error(err)
		}
		numDeleted += res.Data.NumDeleted + numDeleted
	}

	res := struct {
		NumDeleted int `json:"numDeleted"`
	}{
		NumDeleted: numDeleted,
	}
	return res, nil
}

func (api *API) targets(r *http.Request) (interface{}, *apiError) {

	bodys := api.proxyReq(r)
	var data []*Target
	for _, body := range bodys {
		var res targetVals
		err := json.Unmarshal(body, &res)
		if err != nil {
			log.Error(err)
		}
		data = append(data, res.Data.ActiveTargets...)
	}

	res := &TargetDiscovery{ActiveTargets: data}

	return res, nil
}

func (api *API) alertmanagers(r *http.Request) (interface{}, *apiError) {

	bodys := api.proxyReq(r)
	var ams AlertmanagerDiscovery
	for _, body := range bodys {
		var res alertManagerVals
		err := json.Unmarshal(body, &res)
		if err != nil {
			log.Error(err)
		}
		ams.ActiveAlertmanagers = append(ams.ActiveAlertmanagers, res.Data.ActiveAlertmanagers...)
	}

	return ams, nil
}

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

//Duplicate slice, low performence
func (api *API) removeDuplicate(data model.LabelValues) model.LabelValues {
	seen := make(model.LabelValues, 0, len(data))
slice:
	for i, n := range data {
		if i == 0 {
			data = data[:0]
		}
		for _, t := range seen {
			if n == t {
				continue slice
			}
		}
		seen = append(seen, n)
		data = append(data, n)
	}
	return data
}

/*
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

func (api *API) targetRelation() {
		// 这个可以取出__address__和remote prometheus的关系。
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
}
*/
