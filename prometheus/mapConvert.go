package prometheus

import "github.com/prometheus/client_golang/prometheus"

func GetMapConvertPointerCounterVec(mapData map[string]*Metric, mapKey string) *prometheus.CounterVec {
	var metricInterface interface{}
	metricInterface = mapData[mapKey]
	metricData := metricInterface.(*Metric)
	return metricData.MetricCollector.(*prometheus.CounterVec)
}

func GetMapConvertSummary(mapData map[string]*Metric, mapKey string) prometheus.Summary {
	var metricInterface interface{}
	metricInterface = mapData[mapKey]
	metricData := metricInterface.(*Metric)
	return metricData.MetricCollector.(prometheus.Summary)
}
