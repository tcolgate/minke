package minke

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

var metricsProvider MetricsProvider

func SetProvider(p MetricsProvider) {
	workqueue.SetProvider(p)
	cache.SetReflectorMetricsProvider(p)
	metricsProvider = p
}

type SummaryMetric interface {
	Observe(float64)
}

type GaugeMetric interface {
	Set(float64)
}

type CounterMetric interface {
	Inc()
}

type MetricsProvider interface {
	NewListsMetric(name string) cache.CounterMetric
	NewListDurationMetric(name string) cache.SummaryMetric
	NewItemsInListMetric(name string) cache.SummaryMetric

	NewWatchesMetric(name string) cache.CounterMetric
	NewShortWatchesMetric(name string) cache.CounterMetric
	NewWatchDurationMetric(name string) cache.SummaryMetric
	NewItemsInWatchMetric(name string) cache.SummaryMetric

	NewLastResourceVersionMetric(name string) cache.GaugeMetric

	NewDepthMetric(name string) workqueue.GaugeMetric
	NewAddsMetric(name string) workqueue.CounterMetric
	NewLatencyMetric(name string) workqueue.HistogramMetric
	NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric
	NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric
	NewWorkDurationMetric(name string) workqueue.HistogramMetric
	NewRetriesMetric(name string) workqueue.CounterMetric
	NewHTTPTransportMetrics(upstream http.RoundTripper) http.RoundTripper
	NewHTTPServerMetrics(upstream http.Handler) http.Handler
}

type prometheusMetricsProvider struct {
	registry *prometheus.Registry

	listsTotal        *prometheus.CounterVec
	listsDuration     *prometheus.SummaryVec
	itemsPerList      *prometheus.SummaryVec
	watchesTotal      *prometheus.CounterVec
	shortWatchesTotal *prometheus.CounterVec
	watchDuration     *prometheus.SummaryVec
	itemsPerWatch     *prometheus.SummaryVec
	listWatchError    *prometheus.GaugeVec
}

func NewPrometheusMetrics(r *prometheus.Registry) *prometheusMetricsProvider {
	listsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: reflectorSubsystem,
		Name:      "lists_total",
		Help:      "Total number of API lists done by the reflectors",
	}, []string{"name"})

	listsDuration := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "list_duration_seconds",
		Help:      "How long an API list takes to return and decode for the reflectors",
	}, []string{"name"})

	itemsPerList := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "items_per_list",
		Help:      "How many items an API list returns to the reflectors",
	}, []string{"name"})

	watchesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: reflectorSubsystem,
		Name:      "watches_total",
		Help:      "Total number of API watches done by the reflectors",
	}, []string{"name"})

	shortWatchesTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: reflectorSubsystem,
		Name:      "short_watches_total",
		Help:      "Total number of short API watches done by the reflectors",
	}, []string{"name"})

	watchDuration := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "watch_duration_seconds",
		Help:      "How long an API watch takes to return and decode for the reflectors",
	}, []string{"name"})

	itemsPerWatch := prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: reflectorSubsystem,
		Name:      "items_per_watch",
		Help:      "How many items an API watch returns to the reflectors",
	}, []string{"name"})

	listWatchError := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: reflectorSubsystem,
		Name:      "list_watch_error",
		Help:      "Whether or not the reflector received an error on its last list or watch attempt",
	}, []string{"name"})

	p := &prometheusMetricsProvider{
		registry:          r,
		listsTotal:        listsTotal,
		listsDuration:     listsDuration,
		itemsPerList:      itemsPerList,
		watchesTotal:      watchesTotal,
		shortWatchesTotal: shortWatchesTotal,
		watchDuration:     watchDuration,
		itemsPerWatch:     itemsPerWatch,
		listWatchError:    listWatchError,
	}
	p.registry.MustRegister(listsTotal)
	p.registry.MustRegister(listsDuration)
	p.registry.MustRegister(itemsPerList)
	p.registry.MustRegister(watchesTotal)
	p.registry.MustRegister(shortWatchesTotal)
	p.registry.MustRegister(watchDuration)
	p.registry.MustRegister(itemsPerWatch)
	p.registry.MustRegister(listWatchError)

	return p
}

func (p *prometheusMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	depth := prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem:   "workqueue",
		Name:        "depth",
		Help:        "Current depth of workqueue",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(depth)
	return depth
}

func (p *prometheusMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	adds := prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem:   "workqueue",
		Name:        "adds",
		Help:        "Total number of adds handled by workqueue",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(adds)
	return adds
}

func (p *prometheusMetricsProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	latency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Subsystem:   "workqueue",
		Name:        "queue_latency",
		Help:        "How long an item stays in workqueue before being requested.",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(latency)
	return latency
}

func (p *prometheusMetricsProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	workDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Subsystem:   "workqueue",
		Name:        "work_duration",
		Help:        "How long processing an item from workqueue takes.",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(workDuration)
	return workDuration
}

func (p *prometheusMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	retries := prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem:   "workqueue",
		Name:        "retries",
		Help:        "Total number of retries handled by workqueue",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(retries)
	return retries
}

const reflectorSubsystem = "reflector"

func (p *prometheusMetricsProvider) NewListsMetric(name string) cache.CounterMetric {
	return p.listsTotal.WithLabelValues(name)
}

// use summary to get averages and percentiles
func (p *prometheusMetricsProvider) NewListDurationMetric(name string) cache.SummaryMetric {
	return p.listsDuration.WithLabelValues(name)
}

// use summary to get averages and percentiles
func (p *prometheusMetricsProvider) NewItemsInListMetric(name string) cache.SummaryMetric {
	return p.itemsPerList.WithLabelValues(name)
}

func (p *prometheusMetricsProvider) NewWatchesMetric(name string) cache.CounterMetric {
	return p.watchesTotal.WithLabelValues(name)
}

func (p *prometheusMetricsProvider) NewShortWatchesMetric(name string) cache.CounterMetric {
	return p.shortWatchesTotal.WithLabelValues(name)
}

// use summary to get averages and percentiles
func (p *prometheusMetricsProvider) NewWatchDurationMetric(name string) cache.SummaryMetric {
	return p.watchDuration.WithLabelValues(name)
}

// use summary to get averages and percentiles
func (p *prometheusMetricsProvider) NewItemsInWatchMetric(name string) cache.SummaryMetric {
	return p.itemsPerWatch.WithLabelValues(name)
}

func (p *prometheusMetricsProvider) NewLastResourceVersionMetric(name string) cache.GaugeMetric {
	rv := prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem:   reflectorSubsystem,
		Name:        "last_resource_version",
		Help:        "last resource version seen for the reflectors",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(rv)
	return rv
}

func (p *prometheusMetricsProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	rv := prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem:   "workqueue",
		Name:        "longest_running_processor",
		Help:        "How many seconds has the longest running processor for workqueue been running.",
		ConstLabels: prometheus.Labels{"name": name},
	})

	p.registry.MustRegister(rv)
	return rv
}

func (p *prometheusMetricsProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	rv := prometheus.NewGauge(prometheus.GaugeOpts{
		Subsystem: "workqueue",
		Name:      "unfinished_work_seconds",
		Help: "How many seconds of work has done that " +
			"is in progress and hasn't been observed by work_duration. Large " +
			"values indicate stuck threads. One can deduce the number of stuck " +
			"threads by observing the rate at which this increases.",
		ConstLabels: prometheus.Labels{"name": name},
	})
	p.registry.MustRegister(rv)
	return rv
}

func (p *prometheusMetricsProvider) NewListWatchErrorMetric(name string) cache.GaugeMetric {
	return p.listWatchError.WithLabelValues(name)
}

func (p *prometheusMetricsProvider) NewHTTPTransportMetrics(upstream http.RoundTripper) http.RoundTripper {
	inFlightGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_client_inflight_requests",
		Help: "A gauge of in-flight requests for the wrapped client.",
	})

	tlsLatencyVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_client_tls_duration_seconds",
			Help:    "Trace tls latency histogram.",
			Buckets: []float64{.05, .1, .25, .5},
		},
		[]string{"event"},
	)

	histVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_client_request_duration_seconds",
			Help:    "A histogram of request latencies.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"code"},
	)

	p.registry.MustRegister(tlsLatencyVec, histVec, inFlightGauge)

	trace := &promhttp.InstrumentTrace{
		TLSHandshakeStart: func(t float64) {
			tlsLatencyVec.WithLabelValues("tls_handshake_start")
		},
		TLSHandshakeDone: func(t float64) {
			tlsLatencyVec.WithLabelValues("tls_handshake_done")
		},
	}

	// Wrap the default RoundTripper with middleware.
	roundTripper := promhttp.InstrumentRoundTripperInFlight(inFlightGauge,
		promhttp.InstrumentRoundTripperTrace(trace,
			promhttp.InstrumentRoundTripperDuration(histVec, upstream),
		),
	)

	return roundTripper
}

func (p *prometheusMetricsProvider) NewHTTPServerMetrics(upstream http.Handler) http.Handler {
	inFlightGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_server_inflight_requests",
		Help: "A gauge of requests currently being served by the wrapped handler.",
	})

	counter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "A counter for requests to the wrapped handler.",
		},
		[]string{"code"},
	)

	histogramOpts := prometheus.HistogramOpts{
		Name:    "http_server_request_duration_seconds",
		Help:    "A histogram of latencies for requests.",
		Buckets: []float64{.25, .5, 1, 2.5, 5, 10},
	}

	reqDurVec := prometheus.NewHistogramVec(
		histogramOpts,
		[]string{"code"},
	)

	// Register all of the metrics in the standard registry.
	p.registry.MustRegister(inFlightGauge, counter, reqDurVec)

	chain := promhttp.InstrumentHandlerInFlight(inFlightGauge,
		promhttp.InstrumentHandlerCounter(counter,
			promhttp.InstrumentHandlerDuration(reqDurVec,
				upstream,
			),
		),
	)

	return chain
}
