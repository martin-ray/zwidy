package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
)

type Registry struct {
	ConnectedClients      atomic.Int64
	ConnectionsTotal      atomic.Int64
	ConnectionErrorsTotal atomic.Int64
	BytesReceivedTotal    atomic.Int64
	BytesSentTotal        atomic.Int64
	PacketsReceivedTotal  atomic.Int64
	PacketsSentTotal      atomic.Int64
	PacketsDroppedTotal   atomic.Int64
	AuthFailuresTotal     atomic.Int64
	ClientReconnectsTotal atomic.Int64
	Healthy               atomic.Bool
	Role                  string
}

func New(role string) *Registry {
	r := &Registry{Role: role}
	r.Healthy.Store(true)
	return r
	}

func (r *Registry) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "zwidy_connected_clients %d\n", r.ConnectedClients.Load())
		fmt.Fprintf(w, "zwidy_connections_total %d\n", r.ConnectionsTotal.Load())
		fmt.Fprintf(w, "zwidy_connection_errors_total %d\n", r.ConnectionErrorsTotal.Load())
		fmt.Fprintf(w, "zwidy_bytes_received_total %d\n", r.BytesReceivedTotal.Load())
		fmt.Fprintf(w, "zwidy_bytes_sent_total %d\n", r.BytesSentTotal.Load())
		fmt.Fprintf(w, "zwidy_packets_received_total %d\n", r.PacketsReceivedTotal.Load())
		fmt.Fprintf(w, "zwidy_packets_sent_total %d\n", r.PacketsSentTotal.Load())
		fmt.Fprintf(w, "zwidy_packets_dropped_total %d\n", r.PacketsDroppedTotal.Load())
		fmt.Fprintf(w, "zwidy_auth_failures_total %d\n", r.AuthFailuresTotal.Load())
		fmt.Fprintf(w, "zwidy_client_reconnects_total %d\n", r.ClientReconnectsTotal.Load())
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		status := "ok"
		if !r.Healthy.Load() {
			status = "degraded"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprintf(w, "{\"status\":%q,\"role\":%q}\n", status, r.Role)
	})
	return mux
	}
