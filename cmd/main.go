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
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/tinytub/promeproxy/retrieval"
	"github.com/tinytub/promeproxy/web"
)

//type ProxyHandler struct {
//	tm *retrieval.TargetManager
//}
//
//func NewProxyHandle(tm *retrieval.TargetManager) *ProxyHandler {
//	return &ProxyHandler{
//		tm: tm,
//	}
//}
//
//读取配置，计算hash节点，区分不同instan的几个基点请求分别建立连接，并发请求。
//合并结果，写response
//func (ph ProxyHandler) testProxy(w http.ResponseWriter, req *http.Request) {
//	/*
//		client, err := NewClientForTimeOut()
//		fmt.Println(req.URL.Query())
//		if err != nil {
//			fmt.Println(err)
//		}
//		urlStr := "http://360.cn"
//		request, err := http.NewRequest("GET", urlStr, strings.NewReader(""))
//		resp, err := client.Do(request)
//		if err != nil {
//			fmt.Println(err)
//		}
//		body, err := ioutil.ReadAll(resp.Body)
//		if err != nil {
//			fmt.Println(err)
//		}
//		defer resp.Body.Close()
//		w.Write(body)
//	*/
//	//url := "http://10.142.97.138:9090/api/v1/query_range?query=rate(session_rpc_invoke_cnt_c%7Bservice%3D%22qim_test%22,instance%3D~%2210%5C%5C.142%5C%5C.97%5C%5C.138:65130%22%7D%5B1m%5D)&start=1499131459&end=1499135059&step=5"
//	target := ph.tm.Targets
//	fmt.Println(target)
//	fmt.Println(req.RequestURI)
//	client, err := NewClientForTimeOut()
//	if err != nil {
//		fmt.Println(err)
//	}
//	url := "http://10.142.97.138:9090/" + req.RequestURI
//	request, err := http.NewRequest("GET", url, strings.NewReader(""))
//	//resp, err := ctxhttp.Get(ctx, c.httpClient, url.String())
//	resp, err := client.Do(request)
//
//	if resp != nil && resp.Body != nil {
//		defer func() {
//			io.Copy(ioutil.Discard, resp.Body)
//			resp.Body.Close()
//		}()
//	}
//	var respo *response
//	body, err := ioutil.ReadAll(resp.Body)
//	err = json.Unmarshal(body, &respo)
//	fmt.Println(respo)
//}
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

//func GatherWithContext(ctx context.Context, r *http.Request) prometheus.GathererFunc {
//	return func() ([]*dto.MetricFamily, error) {
//		//vs := r.URL.Query()
//		//vs["module"] = vs["module"][1:]
//
//		/*
//			url := &url.URL{
//				Scheme:   c.Scheme,
//				Host:     net.JoinHostPort(c.Address, strconv.Itoa(c.Port)),
//				Path:     c.Path,
//				RawQuery: vs.Encode(),
//			}
//		*/
//		url := "http://10.142.97.138:9090/api/v1/query_range?query=rate(session_rpc_invoke_cnt_c%7Bservice%3D%22qim_test%22,instance%3D~%2210%5C%5C.142%5C%5C.97%5C%5C.138:65130%22%7D%5B1m%5D)&start=1499131459&end=1499135059&step=5"
//		client, err := NewClientForTimeOut()
//		if err != nil {
//			fmt.Println(err)
//		}
//		request, err := http.NewRequest("GET", url, strings.NewReader(""))
//		//resp, err := ctxhttp.Get(ctx, c.httpClient, url.String())
//		resp, err := client.Do(request)
//		/*
//			body, err := ioutil.ReadAll(resp.Body)
//			fmt.Println(string(body))
//		*/
//		if err != nil {
//			/*
//				if glog.V(1) {
//					glog.Errorf("http proxy for module %v failed %+v", c.mcfg.name, err)
//				}
//				proxyErrorCount.WithLabelValues(c.mcfg.name).Inc()
//				if err == context.DeadlineExceeded {
//					proxyTimeoutCount.WithLabelValues(c.mcfg.name).Inc()
//				}
//			*/
//			return nil, err
//		}
//		defer resp.Body.Close()
//
//		dec := expfmt.NewDecoder(resp.Body, expfmt.ResponseFormat(resp.Header))
//
//		result := []*dto.MetricFamily{}
//		for {
//			mf := dto.MetricFamily{}
//			err := dec.Decode(&mf)
//			fmt.Println()
//			if err == io.EOF {
//				break
//			}
//			if err != nil {
//				/*
//					proxyMalformedCount.WithLabelValues(c.mcfg.name).Inc()
//					if glog.V(1) {
//						glog.Errorf("err %+v", err)
//					}
//				*/
//				return nil, err
//			}
//
//			result = append(result, &mf)
//		}
//		fmt.Println(result)
//
//		return result, nil
//	}
//}

type mockSyncer struct {
	sync func(tgs []*config.TargetGroup)
}

func (s *mockSyncer) Sync(tgs []*config.TargetGroup) {
	if s.sync != nil {
		s.sync(tgs)
	}
}

func main() {

	if err := parse(os.Args[1:]); err != nil {
		log.Error(err)
	}
	var (
		sampleAppender = storage.Fanout{}
		reloadables    []Reloadable
	)
	remoteAppender := &remote.Writer{}
	sampleAppender = append(sampleAppender, remoteAppender)
	var (
		targetManager  = retrieval.NewTargetManager(sampleAppender, log.Base())
		ctx, cancelCtx = context.WithCancel(context.Background())
	)

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

	/*
		go func() {
			for {
				aa := targetManager.Targets()
				for k, v := range aa {
					mod := sum64(md5.Sum([]byte(v.Labels()["instance"]))) % 3
					fmt.Println(k, v.DiscoveredLabels(), mod)
				}
				time.Sleep(5 * time.Second)
				fmt.Printf("\n\nloopdone\n\n")
			}
		}()
	*/

	defer cancelCtx()
	go webHandler.Run()

	close(hupReady) // Unblock SIGHUP handler.

	/*
		ph := NewProxyHandle(targetManager)
		r := mux.NewRouter()
		r.HandleFunc("/api/v1/", ph.testProxy)
		//	http.Handle("/", r)
		http.ListenAndServe(":8080", r)
	*/

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
