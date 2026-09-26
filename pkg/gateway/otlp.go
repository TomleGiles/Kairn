package gateway

import (
	"io"
	"net/http"
	"strings"
	"time"

	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/kairn-io/kairn/pkg/ids"
	"github.com/kairn-io/kairn/pkg/model"
)

// Correspondance des métriques OTLP (conventions sémantiques hôte / agent Kairn)
// vers les métriques normalisées Kairn.
var otlpMetricMap = map[string]string{
	"system.cpu.utilization":          model.MetricCPUUtil,
	"system.memory.utilization":       model.MetricMemUtil,
	"system.filesystem.usage":         model.MetricDiskUsedBytes,
	"system.disk.operations.rate":     model.MetricDiskIOPS,
	"system.network.io.receive.rate":  model.MetricNetRxBytesPerSec,
	"system.network.io.transmit.rate": model.MetricNetTxBytesPerSec,
	"kairn.cpu.usage_cores":           model.MetricCPUUsageCores,
	"kairn.memory.usage_bytes":        model.MetricMemUsageBytes,
	"kairn.container.cpu.usage_cores": model.MetricCPUUsageCores,
	"kairn.container.memory.usage":    model.MetricMemUsageBytes,
}

func attr(attrs []*commonpb.KeyValue, key string) string {
	for _, a := range attrs {
		if a.GetKey() == key {
			return a.GetValue().GetStringValue()
		}
	}
	return ""
}

// otlpMetrics reçoit des métriques OTLP/HTTP (protobuf ou JSON) de l'agent.
func (g *Gateway) otlpMetrics(w http.ResponseWriter, r *http.Request) {
	ctx, c, err := g.agentConnector(r)
	if err != nil || c.Type != "agent" {
		problem(w, http.StatusUnauthorized, "invalid agent token")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
	if err != nil {
		problem(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	var req colmetrics.ExportMetricsServiceRequest
	if strings.Contains(r.Header.Get("Content-Type"), "json") {
		err = protojson.Unmarshal(body, &req)
	} else {
		err = proto.Unmarshal(body, &req)
	}
	if err != nil {
		problem(w, http.StatusBadRequest, "invalid OTLP payload")
		return
	}
	pts := ConvertOTLP(c.OrgID, c.ID, &req)
	if len(pts) > 0 {
		if err := g.TSDB.WriteMetrics(ctx, pts); err != nil {
			problem(w, http.StatusInternalServerError, "storage error")
			return
		}
	}
	resp, _ := proto.Marshal(&colmetrics.ExportMetricsServiceResponse{})
	w.Header().Set("Content-Type", "application/x-protobuf")
	_, _ = w.Write(resp)
}

// ConvertOTLP convertit une requête OTLP en points Kairn. La ressource est
// identifiée par l'attribut host.id (ou host.name) et, pour les conteneurs,
// container.id.
func ConvertOTLP(orgID, connectorID string, req *colmetrics.ExportMetricsServiceRequest) []model.MetricPoint {
	var out []model.MetricPoint
	for _, rm := range req.GetResourceMetrics() {
		rattrs := rm.GetResource().GetAttributes()
		host := attr(rattrs, "host.id")
		if host == "" {
			host = attr(rattrs, "host.name")
		}
		if host == "" {
			continue
		}
		hostID := ids.Resource(orgID, connectorID, model.TypeHost, host)
		for _, sm := range rm.GetScopeMetrics() {
			for _, m := range sm.GetMetrics() {
				name, ok := otlpMetricMap[m.GetName()]
				if !ok {
					continue
				}
				var dps []*metricspb.NumberDataPoint
				switch d := m.GetData().(type) {
				case *metricspb.Metric_Gauge:
					dps = d.Gauge.GetDataPoints()
				case *metricspb.Metric_Sum:
					dps = d.Sum.GetDataPoints()
				}
				for _, dp := range dps {
					v := dp.GetAsDouble()
					if _, isInt := dp.GetValue().(*metricspb.NumberDataPoint_AsInt); isInt {
						v = float64(dp.GetAsInt())
					}
					res := hostID
					if cid := attr(dp.GetAttributes(), "container.id"); cid != "" {
						res = ids.Resource(orgID, connectorID, model.TypeK8sPod, cid)
					}
					out = append(out, model.MetricPoint{OrgID: orgID, ResourceID: res, Metric: name,
						TS: time.Unix(0, int64(dp.GetTimeUnixNano())).UTC(), Value: v})
				}
			}
		}
	}
	return out
}
