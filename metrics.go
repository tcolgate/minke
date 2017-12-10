package minke

import (
	"github.com/prometheus/client_golang/prometheus"
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
	NewLatencyMetric(name string) workqueue.SummaryMetric
	NewWorkDurationMetric(name string) workqueue.SummaryMetric
	NewRetriesMetric(name string) workqueue.CounterMetric
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
		Subsystem: name,
		Name:      "depth",
		Help:      "Current depth of workqueue: " + name,
	})
	p.registry.Register(depth)
	return depth
}

func (p *prometheusMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	adds := prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: name,
		Name:      "adds",
		Help:      "Total number of adds handled by workqueue: " + name,
	})
	p.registry.Register(adds)
	return adds
}

func (p *prometheusMetricsProvider) NewLatencyMetric(name string) workqueue.SummaryMetric {
	latency := prometheus.NewSummary(prometheus.SummaryOpts{
		Subsystem: name,
		Name:      "queue_latency",
		Help:      "How long an item stays in workqueue" + name + " before being requested.",
	})
	p.registry.Register(latency)
	return latency
}

func (p *prometheusMetricsProvider) NewWorkDurationMetric(name string) workqueue.SummaryMetric {
	workDuration := prometheus.NewSummary(prometheus.SummaryOpts{
		Subsystem: name,
		Name:      "work_duration",
		Help:      "How long processing an item from workqueue" + name + " takes.",
	})
	p.registry.Register(workDuration)
	return workDuration
}

func (p *prometheusMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	retries := prometheus.NewCounter(prometheus.CounterOpts{
		Subsystem: name,
		Name:      "retries",
		Help:      "Total number of retries handled by workqueue: " + name,
	})
	p.registry.Register(retries)
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
		Subsystem: name,
		Name:      "last_resource_version",
		Help:      "last resource version seen for the reflectors",
	})
	p.registry.MustRegister(rv)
	return rv
}

func (p *prometheusMetricsProvider) NewListWatchErrorMetric(name string) cache.GaugeMetric {
	return p.listWatchError.WithLabelValues(name)
}
