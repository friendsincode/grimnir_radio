package telemetry

import "net/http"

// Handler exposes metrics endpoint.
func Handler() http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        _, _ = w.Write([]byte("# HELP grimnir_radio_metrics Placeholder\n"))
    })
}
