package metrics

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/types"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/system"
)

type ForecastData struct {
	ForecastWindow int32     `json:"forecast_window"`
	ConcTrace      []float64 `json:"conc_trace"`
}

type ForecastResult struct {
	ForecastedTrace []float64 `json:"forecasted_trace"`
}

func ForecastHandler(forecast_window int32, conc_trace []float64, key types.NamespacedName) ForecastResult {

	values := ForecastData{
		ForecastWindow: forecast_window,
		ConcTrace:      conc_trace,
	}
	json_data, err := json.Marshal(values)
	if err != nil {
		fmt.Println("returning empty response: marshalling failed")
		return ForecastResult{}
	}

	forecasterEndpoint := "http://" + pkgnet.GetServiceHostname("forecaster", system.Namespace()) + ":5000"
	forecastEp := forecasterEndpoint + "/forecast/" + key.Namespace + "-" + key.Name

	hash := md5.Sum([]byte(key.String()))

	// takes in the first 4 elements and converts them to int
	hashInt := binary.BigEndian.Uint32(hash[:4])

	// number of pod yamls specified
	numberOfForecasterPods := uint32(2)

	// currently 0 or 1 supported
	podNumber := hashInt % numberOfForecasterPods

	if podNumber == 0 {
		forecastEp = forecasterEndpoint + "/forecast/" + key.Namespace + "-" + key.Name
	} else if podNumber != 0 {
		forecasterEndpoint = "http://" + pkgnet.GetServiceHostname(fmt.Sprintf("forecaster%d", podNumber+1), system.Namespace()) + ":5000"
		forecastEp = forecasterEndpoint + fmt.Sprintf("/forecast%d/", podNumber+1) + key.Namespace + "-" + key.Name
	}

	fmt.Println(forecastEp)

	client := &http.Client{
		Timeout: time.Millisecond * 20000,
	}

	req, err := http.NewRequest("POST", forecastEp, bytes.NewBuffer(json_data))

	if err != nil {
		fmt.Println("returning empty response: because of client timeout")
		return ForecastResult{}
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("returning empty response: forecaster did not respond")
		return ForecastResult{}
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)

	defer resp.Body.Close()

	if err != nil {
		fmt.Println("returning empty response: reading bytes failed")
		return ForecastResult{}
	}

	resp.Close = true

	var res ForecastResult
	json.Unmarshal(bodyBytes, &res)

	fmt.Println("we got the forecasted result ", res.ForecastedTrace)
	return res
}
