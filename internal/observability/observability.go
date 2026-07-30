package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

const readinessTimeout = 500 * time.Millisecond

// ReadinessFunc checks one required dependency within the supplied context.
type ReadinessFunc func(context.Context) error

// Service owns the private metrics registry, probes, and API instrumentation.
type Service struct {
	registry       *prometheus.Registry
	readiness      ReadinessFunc
	httpRequests   *prometheus.CounterVec
	httpDuration   *prometheus.HistogramVec
	metricsHandler http.Handler
}

// New creates an observability service backed by the relay Redis client.
func New(version string, redisClient *redis.Client, snapshots ...RuntimeSnapshotFunc) *Service {
	readiness := func(context.Context) error {
		return errors.New("Redis client is unavailable")
	}
	if redisClient != nil {
		readiness = func(ctx context.Context) error {
			return redisClient.Ping(ctx).Err()
		}
	}
	var snapshot RuntimeSnapshotFunc
	if len(snapshots) > 0 {
		snapshot = snapshots[0]
	}
	return newServiceWithOperational(version, readiness, redisClient, snapshot)
}

func newService(version string, readiness ReadinessFunc) *Service {
	return newServiceWithOperational(version, readiness, nil, nil)
}

func newServiceWithOperational(
	version string,
	readiness ReadinessFunc,
	redisClient *redis.Client,
	snapshot RuntimeSnapshotFunc,
) *Service {
	if readiness == nil {
		readiness = func(context.Context) error {
			return errors.New("readiness check is unavailable")
		}
	}

	service := &Service{
		registry:  prometheus.NewRegistry(),
		readiness: readiness,
		httpRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "activity_relay",
				Subsystem: "http",
				Name:      "requests_total",
				Help:      "Total HTTP requests handled by the public relay API.",
			},
			[]string{"method", "route", "code"},
		),
		httpDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "activity_relay",
				Subsystem: "http",
				Name:      "request_duration_seconds",
				Help:      "Public relay API request duration in seconds.",
				Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "route", "code"},
		),
	}

	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "activity_relay",
			Name:      "build_info",
			Help:      "Build information for the running Activity-Relay process.",
		},
		[]string{"version"},
	)
	buildInfo.WithLabelValues(version).Set(1)

	dependencyReady := prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace:   "activity_relay",
			Name:        "dependency_ready",
			Help:        "Whether a required Activity-Relay dependency is ready.",
			ConstLabels: prometheus.Labels{"dependency": "redis"},
		},
		func() float64 {
			if service.checkReadiness(context.Background()) == nil {
				return 1
			}
			return 0
		},
	)

	service.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		buildInfo,
		dependencyReady,
		service.httpRequests,
		service.httpDuration,
	)
	if operationalCollector := NewOperationalCollector(redisClient, snapshot); operationalCollector != nil {
		service.registry.MustRegister(operationalCollector)
	}
	service.metricsHandler = promhttp.HandlerFor(
		service.registry,
		promhttp.HandlerOpts{EnableOpenMetrics: true},
	)

	return service
}

// Handler returns the private observability surface.
func (service *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", readOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		service.metricsHandler.ServeHTTP(w, r)
	})))
	mux.Handle("/-/healthy", readOnly(http.HandlerFunc(service.handleHealthy)))
	mux.Handle("/-/ready", readOnly(http.HandlerFunc(service.handleReady)))
	return mux
}

// Instrument records bounded HTTP metrics for the public relay API.
func (service *Service) Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		method := normalizedMethod(r.Method)
		route := normalizedRoute(r.URL.Path)

		defer func() {
			status := recorder.status
			if recovered := recover(); recovered != nil {
				if status == 0 {
					status = http.StatusInternalServerError
				}
				service.observeRequest(method, route, status, started)
				panic(recovered)
			}
			if status == 0 {
				status = http.StatusOK
			}
			service.observeRequest(method, route, status, started)
		}()

		next.ServeHTTP(recorder, r)
	})
}

func (service *Service) observeRequest(method string, route string, status int, started time.Time) {
	code := normalizedStatusCode(status)
	service.httpRequests.WithLabelValues(method, route, code).Inc()
	service.httpDuration.WithLabelValues(method, route, code).Observe(time.Since(started).Seconds())
}

func (service *Service) handleHealthy(w http.ResponseWriter, r *http.Request) {
	writeProbe(w, r, http.StatusOK, "healthy\n")
}

func (service *Service) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := service.checkReadiness(r.Context()); err != nil {
		writeProbe(w, r, http.StatusServiceUnavailable, "not ready\n")
		return
	}
	writeProbe(w, r, http.StatusOK, "ready\n")
}

func (service *Service) checkReadiness(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, readinessTimeout)
	defer cancel()
	return service.readiness(ctx)
}

func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeProbe(w, r, http.StatusMethodNotAllowed, "method not allowed\n")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeProbe(w http.ResponseWriter, r *http.Request, status int, body string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = fmt.Fprint(w, body)
}

func normalizedStatusCode(status int) string {
	if status < 100 || status > 599 {
		return "other"
	}
	return strconv.Itoa(status)
}

func normalizedMethod(method string) string {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func normalizedRoute(path string) string {
	switch path {
	case "/.well-known/nodeinfo",
		"/.well-known/webfinger",
		"/nodeinfo/2.1",
		"/status.json",
		"/actor",
		"/actor/outbox",
		"/actor/followers",
		"/actor/following",
		"/inbox":
		return path
	default:
		return "unknown"
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if recorder.status != 0 {
		return
	}
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(body []byte) (int, error) {
	if recorder.status == 0 {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(body)
}

// Unwrap allows http.ResponseController to reach the original writer.
func (recorder *statusRecorder) Unwrap() http.ResponseWriter {
	return recorder.ResponseWriter
}
