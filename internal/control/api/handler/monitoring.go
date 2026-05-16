package handler

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/taichirain/portkey/internal/config"
	"go.uber.org/zap"
)

type StatusDistribution struct {
	Status2xx int64 `json:"2xx"`
	Status3xx int64 `json:"3xx"`
	Status4xx int64 `json:"4xx"`
	Status5xx int64 `json:"5xx"`
}

type DPMetricsResponse struct {
	RequestsTotal      int64              `json:"requests_total"`
	RequestsActive     int64              `json:"requests_active"`
	ResponseLatency    int64              `json:"response_latency_us"`
	ErrorsTotal        int64              `json:"errors_total"`
	QPS1m              float64            `json:"qps_1m"`
	ErrorRate1m        float64            `json:"error_rate_1m"`
	AvgLatencyMs1m     float64            `json:"avg_latency_ms_1m"`
	RateLimitedTotal   int64              `json:"rate_limited_total"`
	PolicyHitTotal     int64              `json:"policy_hit_total"`
	UptimeSeconds      float64            `json:"uptime_seconds"`
	StatusDistribution StatusDistribution `json:"status_distribution"`
}

type DPInstanceMetrics struct {
	Name               string             `json:"name"`
	URL                string             `json:"url"`
	Online             bool               `json:"online"`
	QPS1m              float64            `json:"qps_1m"`
	ErrorRate1m        float64            `json:"error_rate_1m"`
	AvgLatencyMs1m     float64            `json:"avg_latency_ms_1m"`
	RequestsActive     int64              `json:"requests_active"`
	RequestsTotal      int64              `json:"requests_total"`
	ErrorsTotal        int64              `json:"errors_total"`
	RateLimitedTotal   int64              `json:"rate_limited_total"`
	PolicyHitTotal     int64              `json:"policy_hit_total"`
	StatusDistribution StatusDistribution `json:"status_distribution"`
	RevisionMismatch   bool               `json:"revision_mismatch,omitempty"`
	UptimeSeconds      float64            `json:"uptime_seconds"`
}

type AggregatedMetrics struct {
	QPS1m              float64            `json:"qps_1m"`
	ErrorRate1m        float64            `json:"error_rate_1m"`
	AvgLatencyMs1m     float64            `json:"avg_latency_ms_1m"`
	RequestsActive     int64              `json:"requests_active"`
	RequestsTotal      int64              `json:"requests_total"`
	ErrorsTotal        int64              `json:"errors_total"`
	RateLimitedTotal   int64              `json:"rate_limited_total"`
	PolicyHitTotal     int64              `json:"policy_hit_total"`
	StatusDistribution StatusDistribution `json:"status_distribution"`
}

type ActiveRevisionInfo struct {
	ID          string     `json:"id"`
	Version     string     `json:"version"`
	PublishedAt *time.Time `json:"published_at"`
}

type MonitoringMetricsResponse struct {
	Aggregated     AggregatedMetrics    `json:"aggregated"`
	PerDP          []DPInstanceMetrics  `json:"per_dp"`
	ActiveRevision *ActiveRevisionInfo  `json:"active_revision,omitempty"`
}

type DPStatusInstance struct {
	Name              string         `json:"name"`
	URL               string         `json:"url"`
	Online            bool           `json:"online"`
	RevisionID        string         `json:"revision_id"`
	RevisionMismatch  bool           `json:"revision_mismatch"`
	UptimeSeconds     float64        `json:"uptime_seconds"`
	ConfigStats       ConfigStats    `json:"config_stats"`
}

type ConfigStats struct {
	ServicesCount       int `json:"services_count"`
	RoutesCount         int `json:"routes_count"`
	UpstreamsCount      int `json:"upstreams_count"`
	TrafficPoliciesCount int `json:"traffic_policies_count"`
}

type DPStatusResponse struct {
	CPActiveRevisionID      string              `json:"cp_active_revision_id"`
	CPActiveRevisionVersion string              `json:"cp_active_revision_version"`
	DPInstances             []DPStatusInstance  `json:"dp_instances"`
}

type DPRevisionResponse struct {
	CurrentRevisionID string      `json:"current_revision_id"`
	HasRevision       bool        `json:"has_revision"`
	ConfigStats       ConfigStats `json:"config_stats"`
	LastUpdated       *time.Time  `json:"last_updated,omitempty"`
}

type MonitoringHandler struct {
	cfg          *config.Config
	logger       *zap.Logger
	httpClient   *http.Client
	getActiveRev func() (string, string, *time.Time)
}

func NewMonitoringHandler(cfg *config.Config, logger *zap.Logger, getActiveRev func() (string, string, *time.Time)) *MonitoringHandler {
	return &MonitoringHandler{
		cfg: cfg,
		logger: logger,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
		getActiveRev: getActiveRev,
	}
}

func (h *MonitoringHandler) fetchDPMetrics(instance config.DPInstance) (DPInstanceMetrics, *DPMetricsResponse) {
	result := DPInstanceMetrics{
		Name:   instance.Name,
		URL:    instance.URL,
		Online: false,
	}

	resp, err := h.httpClient.Get(instance.URL + "/metrics")
	if err != nil {
		h.logger.Debug("Failed to fetch DP metrics",
			zap.String("name", instance.Name),
			zap.String("url", instance.URL),
			zap.Error(err),
		)
		return result, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		h.logger.Debug("DP metrics returned non-200",
			zap.String("name", instance.Name),
			zap.Int("status", resp.StatusCode),
		)
		return result, nil
	}

	var dpMetrics DPMetricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&dpMetrics); err != nil {
		h.logger.Debug("Failed to decode DP metrics",
			zap.String("name", instance.Name),
			zap.Error(err),
		)
		return result, nil
	}

	result.Online = true
	result.QPS1m = dpMetrics.QPS1m
	result.ErrorRate1m = dpMetrics.ErrorRate1m
	result.AvgLatencyMs1m = dpMetrics.AvgLatencyMs1m
	result.RequestsActive = dpMetrics.RequestsActive
	result.RequestsTotal = dpMetrics.RequestsTotal
	result.ErrorsTotal = dpMetrics.ErrorsTotal
	result.RateLimitedTotal = dpMetrics.RateLimitedTotal
	result.PolicyHitTotal = dpMetrics.PolicyHitTotal
	result.StatusDistribution = dpMetrics.StatusDistribution
	result.UptimeSeconds = dpMetrics.UptimeSeconds

	return result, &dpMetrics
}

func (h *MonitoringHandler) fetchDPRevision(instance config.DPInstance) (string, ConfigStats, bool) {
	resp, err := h.httpClient.Get(instance.URL + "/revision")
	if err != nil {
		return "", ConfigStats{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", ConfigStats{}, false
	}

	var revResp DPRevisionResponse
	if err := json.NewDecoder(resp.Body).Decode(&revResp); err != nil {
		return "", ConfigStats{}, false
	}

	return revResp.CurrentRevisionID, revResp.ConfigStats, true
}

func (h *MonitoringHandler) aggregateMetrics(allMetrics []DPInstanceMetrics, allDPResponses []*DPMetricsResponse) AggregatedMetrics {
	var aggregated AggregatedMetrics
	var totalRequestsForLatency int64
	var totalWeightedLatency float64
	var totalRequestsInWindow int64
	var totalErrorsInWindow int64

	for i, m := range allMetrics {
		if !m.Online {
			continue
		}

		aggregated.RequestsActive += m.RequestsActive
		aggregated.RequestsTotal += m.RequestsTotal
		aggregated.ErrorsTotal += m.ErrorsTotal
		aggregated.RateLimitedTotal += m.RateLimitedTotal
		aggregated.PolicyHitTotal += m.PolicyHitTotal
		aggregated.StatusDistribution.Status2xx += m.StatusDistribution.Status2xx
		aggregated.StatusDistribution.Status3xx += m.StatusDistribution.Status3xx
		aggregated.StatusDistribution.Status4xx += m.StatusDistribution.Status4xx
		aggregated.StatusDistribution.Status5xx += m.StatusDistribution.Status5xx

		aggregated.QPS1m += m.QPS1m

		if allDPResponses[i] != nil {
			requests := allDPResponses[i].RequestsTotal
			if requests > 0 {
				totalRequestsForLatency += requests
				totalWeightedLatency += m.AvgLatencyMs1m * float64(requests)
			}

			errorRate := allDPResponses[i].ErrorRate1m
			qps := allDPResponses[i].QPS1m
			if qps > 0 {
				requestsInWindow := int64(qps * 60)
				totalRequestsInWindow += requestsInWindow
				totalErrorsInWindow += int64(float64(requestsInWindow) * errorRate)
			}
		}
	}

	if totalRequestsForLatency > 0 {
		aggregated.AvgLatencyMs1m = totalWeightedLatency / float64(totalRequestsForLatency)
	}

	if totalRequestsInWindow > 0 {
		aggregated.ErrorRate1m = float64(totalErrorsInWindow) / float64(totalRequestsInWindow)
	}

	return aggregated
}

func (h *MonitoringHandler) GetMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	instances := h.cfg.DPInstances
	if len(instances) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MonitoringMetricsResponse{
			Aggregated: AggregatedMetrics{},
			PerDP:      []DPInstanceMetrics{},
		})
		return
	}

	var wg sync.WaitGroup
	allMetrics := make([]DPInstanceMetrics, len(instances))
	allDPResponses := make([]*DPMetricsResponse, len(instances))

	for i, instance := range instances {
		wg.Add(1)
		go func(idx int, inst config.DPInstance) {
			defer wg.Done()
			metrics, resp := h.fetchDPMetrics(inst)
			allMetrics[idx] = metrics
			allDPResponses[idx] = resp
		}(i, instance)
	}

	wg.Wait()

	var cpRevID, cpRevVersion string
	var cpRevPublishedAt *time.Time
	if h.getActiveRev != nil {
		cpRevID, cpRevVersion, cpRevPublishedAt = h.getActiveRev()
	}

	for i := range allMetrics {
		if allMetrics[i].Online && cpRevID != "" {
			revID, _, ok := h.fetchDPRevision(instances[i])
			if ok && revID != cpRevID {
				allMetrics[i].RevisionMismatch = true
			}
		}
	}

	aggregated := h.aggregateMetrics(allMetrics, allDPResponses)

	response := MonitoringMetricsResponse{
		Aggregated: aggregated,
		PerDP:      allMetrics,
	}

	if cpRevID != "" {
		response.ActiveRevision = &ActiveRevisionInfo{
			ID:          cpRevID,
			Version:     cpRevVersion,
			PublishedAt: cpRevPublishedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}

func (h *MonitoringHandler) GetDPStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	instances := h.cfg.DPInstances

	var cpRevID, cpRevVersion string
	if h.getActiveRev != nil {
		cpRevID, cpRevVersion, _ = h.getActiveRev()
	}

	var wg sync.WaitGroup
	dpStatuses := make([]DPStatusInstance, len(instances))

	for i, instance := range instances {
		wg.Add(1)
		go func(idx int, inst config.DPInstance) {
			defer wg.Done()

			status := DPStatusInstance{
				Name:   inst.Name,
				URL:    inst.URL,
				Online: false,
			}

			revID, configStats, ok := h.fetchDPRevision(inst)
			if ok {
				status.Online = true
				status.RevisionID = revID
				status.ConfigStats = configStats
				if cpRevID != "" && revID != cpRevID {
					status.RevisionMismatch = true
				}
			}

			metrics, _ := h.fetchDPMetrics(inst)
			if metrics.Online {
				status.UptimeSeconds = metrics.UptimeSeconds
			}

			dpStatuses[idx] = status
		}(i, instance)
	}

	wg.Wait()

	response := DPStatusResponse{
		CPActiveRevisionID:      cpRevID,
		CPActiveRevisionVersion: cpRevVersion,
		DPInstances:             dpStatuses,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(response)
}
