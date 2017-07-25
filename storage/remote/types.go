package remote

import "github.com/prometheus/common/model"

type queryRes struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`

		Result model.Matrix `json:"result"`
	} `json:"data"`
}
