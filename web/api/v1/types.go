package v1

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/tinytub/promeproxy/retrieval"
)

type labalVals struct {
	Status string            `json:"status"`
	Data   model.LabelValues `json:"data"`
}
type seriesVals struct {
	Data   []model.Metric `json:"data"`
	Status string         `json:"status"`
}

type seriesDelVals struct {
	Status string `json:"status"`
	Data   struct {
		NumDeleted int `json:"numDeleted"`
	} `json:"data"`
}

type targetVals struct {
	Status string `json:"status"`
	Data   struct {
		ActiveTargets []*Target `json:"activeTargets"`
	} `json:"data"`
}

type Target struct {
	DiscoveredLabels model.LabelSet `json:"discoveredLabels"`
	Labels           model.LabelSet `json:"labels"`

	ScrapeURL string `json:"scrapeUrl"`

	LastError  string                 `json:"lastError"`
	LastScrape time.Time              `json:"lastScrape"`
	Health     retrieval.TargetHealth `json:"health"`
}

type TargetDiscovery struct {
	ActiveTargets []*Target `json:"activeTargets"`
}

type alertManagerVals struct {
	Data struct {
		AlertmanagerDiscovery
	} `json:"data"`
	Status string `json:"status"`
}

type AlertmanagerDiscovery struct {
	ActiveAlertmanagers []*AlertmanagerTarget `json:"activeAlertmanagers"`
}

type AlertmanagerTarget struct {
	URL string `json:"url"`
}

type queryRes struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`

		Result matrix `json:"result"`
	} `json:"data"`
}

type sampleStream struct {
	Metric model.Metric       `json:"metric"`
	Values []model.SamplePair `json:"values"`
}

type matrix []*sampleStream

/*
func (matrix) Type() model.ValueType { return model.ValMatrix }
func (mat matrix) String() string    { return mat.value().String() }
*/

func (mat matrix) value() model.Matrix {
	val := make(model.Matrix, len(mat))
	for i, ss := range mat {
		val[i] = &model.SampleStream{
			Metric: ss.Metric,
			Values: ss.Values,
		}
	}
	return val
}
