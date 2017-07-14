package v1

import "github.com/prometheus/common/model"

type queryRes struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		/*
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric struct {
					Instance string `json:"instance"`
					Job      string `json:"job"`
					Service  string `json:"service"`
					URI      string `json:"uri"`
				} `json:"metric"`
				Values []struct {
					Num0 int    `json:"0"`
					Num1 string `json:"1"`
				} `json:"values"`
			} `json:"result"`
		*/
		//model.Matrix
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
