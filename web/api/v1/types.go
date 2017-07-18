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

type alertManagerVals struct {
	Data struct {
		AlertmanagerDiscovery
	} `json:"data"`
	Status string `json:"status"`
}

// AlertmanagerDiscovery has all the active Alertmanagers.
type AlertmanagerDiscovery struct {
	ActiveAlertmanagers []*AlertmanagerTarget `json:"activeAlertmanagers"`
}

// AlertmanagerTarget has info on one AM.
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

// sampleStream is a stream of Values belonging to an attached COWMetric.
type sampleStream struct {
	Metric model.Metric       `json:"metric"`
	Values []model.SamplePair `json:"values"`
}

// matrix is a slice of SampleStreams that implements sort.Interface and
// has a String method.
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
