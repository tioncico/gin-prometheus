package prometheus

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"strconv"
	"time"
)

var defaultMetricPath = "/metrics"

// Metric is a definition for the name, description, type, ID, and
// prometheus.Collector type (i.e. CounterVec, Summary, etc) of each metric
type Metric struct {
	MetricCollector prometheus.Collector
	ID              string
	Name            string
	Description     string
	Type            MetricType
	Args            []string
}
type MetricType string

var (
	METRIC_TYPE_COUNTER_VEC   MetricType = "counter_vec"
	METRIC_TYPE_COUNTER       MetricType = "counter"
	METRIC_TYPE_GAUGE_VEC     MetricType = "gauge_vec"
	METRIC_TYPE_GAUGE         MetricType = "gauge"
	METRIC_TYPE_HISTOGRAM_VEC MetricType = "histogram_vec"
	METRIC_TYPE_HISTOGRAM     MetricType = "histogram"
	METRIC_TYPE_SUMMARY_VEC   MetricType = "summary_vec"
	METRIC_TYPE_SUMMARY       MetricType = "summary"
)

// Prometheus contains the metrics gathered by the instance and its path
type Prometheus struct {
	router        *gin.Engine
	listenAddress string
	PushGateway   PushGateway

	MetricsList map[string]*Metric
	MetricsPath string

	ReqCntURLLabelMappingFn RequestCounterURLLabelMappingFn

	// gin.Context string to use as a prometheus URL label
	URLLabelFromContext string
}

// PrometheusPushGateway contains the configuration for pushing to a Prometheus pushgateway (optional)
type PushGateway struct {

	// Push interval in seconds
	PushIntervalSeconds time.Duration

	// Push Gateway URL in format http://domain:port
	// where JOBNAME can be any string of your choice
	PushGatewayURL string

	// Local metrics URL where metrics are fetched from, this could be ommited in the future
	// if implemented using prometheus common/expfmt instead
	MetricsURL string

	// pushgateway job name, defaults to "gin"
	Job string
}

var standardMetrics = map[string]*Metric{
	"reqCnt": &Metric{
		ID:          "reqCnt",
		Name:        "requests_total",
		Description: "How many HTTP requests processed, partitioned by status code and HTTP method.",
		Type:        METRIC_TYPE_COUNTER_VEC,
		Args:        []string{"code", "method", "handler", "host", "url"},
	},
	"reqDur": &Metric{
		ID:          "reqDur",
		Name:        "request_duration_seconds",
		Description: "The HTTP request latencies in seconds.",
		Type:        METRIC_TYPE_SUMMARY,
	},
	"resSz": &Metric{
		ID:          "resSz",
		Name:        "response_size_bytes",
		Description: "The HTTP response sizes in bytes.",
		Type:        METRIC_TYPE_SUMMARY,
	},
	"reqSz": &Metric{
		ID:          "reqSz",
		Name:        "request_size_bytes",
		Description: "The HTTP request sizes in bytes.",
		Type:        METRIC_TYPE_SUMMARY,
	},
}

type RequestCounterURLLabelMappingFn func(c *gin.Context) string

// NewPrometheus generates a new set of metrics with a certain subsystem name
func NewPrometheus(subsystemName string, customMetricsMap ...map[string]*Metric) *Prometheus {

	metricsList := make(map[string]*Metric)
	if len(customMetricsMap) >= 1 {
		metricsList = customMetricsMap[0]
	}

	for key, metric := range standardMetrics {
		metricsList[key] = metric
	}

	p := &Prometheus{
		MetricsList: metricsList,
		MetricsPath: defaultMetricPath,
		ReqCntURLLabelMappingFn: func(c *gin.Context) string {
			return c.Request.URL.String() // i.e. by default do nothing, i.e. return URL as is
		},
	}

	p.registerMetrics(subsystemName)

	return p
}

func (p *Prometheus) registerMetrics(subsystem string) {
	for _, metricDef := range p.MetricsList {
		metric := NewMetric(metricDef, subsystem)
		if err := prometheus.Register(metric); err != nil {
			log("%s could not be registered in Prometheus", metricDef.Name)
			continue
		}
		metricDef.MetricCollector = metric
	}
}

// NewMetric associates prometheus.Collector based on Metric.Type
func NewMetric(m *Metric, subsystem string) prometheus.Collector {
	var metric prometheus.Collector
	switch m.Type {
	case METRIC_TYPE_COUNTER_VEC:
		metric = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case METRIC_TYPE_COUNTER:
		metric = prometheus.NewCounter(
			prometheus.CounterOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	case METRIC_TYPE_GAUGE_VEC:
		metric = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case METRIC_TYPE_GAUGE:
		metric = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	case METRIC_TYPE_HISTOGRAM_VEC:
		metric = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case METRIC_TYPE_HISTOGRAM:
		metric = prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	case METRIC_TYPE_SUMMARY_VEC:
		metric = prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case METRIC_TYPE_SUMMARY:
		metric = prometheus.NewSummary(
			prometheus.SummaryOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	}
	return metric
}

// Use adds the middleware to a gin engine.
func (p *Prometheus) Use(e *gin.Engine) {
	e.Use(p.HandlerFunc())
	p.SetMetricsPath(e)
}

// HandlerFunc defines handler function for middleware
func (p *Prometheus) HandlerFunc() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.String() == p.MetricsPath {
			c.Next()
			return
		}

		start := time.Now()
		reqSz := computeApproximateRequestSize(c.Request)

		c.Next()

		status := strconv.Itoa(c.Writer.Status())
		elapsed := float64(time.Since(start)) / float64(time.Second)
		resSz := float64(c.Writer.Size())

		reqDur := GetMapConvertSummary(p.MetricsList, "reqDur")
		reqDur.Observe(elapsed)
		//p.reqDur.Observe(elapsed)
		url := p.ReqCntURLLabelMappingFn(c)
		// jlambert Oct 2018 - sidecar specific mod
		if len(p.URLLabelFromContext) > 0 {
			u, found := c.Get(p.URLLabelFromContext)
			if !found {
				u = "unknown"
			}
			url = u.(string)
		}

		reqCnt := GetMapConvertPointerCounterVec(p.MetricsList, "reqCnt")
		reqCnt.WithLabelValues(status, c.Request.Method, c.HandlerName(), c.Request.Host, url).Inc()
		reqSzP := GetMapConvertSummary(p.MetricsList, "reqSz")
		reqSzP.Observe(float64(reqSz))
		resSzP := GetMapConvertSummary(p.MetricsList, "resSz")
		resSzP.Observe(resSz)
	}
}

func (p *Prometheus) SetListenAddress(address string) {
	p.listenAddress = address
	if p.listenAddress != "" {
		p.router = gin.Default()
	}
}

// SetMetricsPath set metrics paths
func (p *Prometheus) SetMetricsPath(e *gin.Engine) {
	if p.listenAddress != "" {
		p.router.GET(p.MetricsPath, prometheusHandler())
		p.runServer()
	} else {
		e.GET(p.MetricsPath, prometheusHandler())
	}
}
func (p *Prometheus) runServer() {
	if p.listenAddress != "" {
		go p.router.Run(p.listenAddress)
	}
}

// From https://github.com/DanielHeckrath/gin-prometheus/blob/master/gin_prometheus.go
func computeApproximateRequestSize(r *http.Request) int {
	s := 0
	if r.URL != nil {
		s = len(r.URL.String())
	}

	s += len(r.Method)
	s += len(r.Proto)
	for name, values := range r.Header {
		s += len(name)
		for _, value := range values {
			s += len(value)
		}
	}
	s += len(r.Host)

	// N.B. r.Form and r.MultipartForm are assumed to be included in r.URL.

	if r.ContentLength != -1 {
		s += int(r.ContentLength)
	}
	return s
}

func prometheusHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

func log(format string, a ...interface{}) {
	fmt.Println(time.Now().String() + " :" + "etcd:" + fmt.Sprintf(format, a...))
}
