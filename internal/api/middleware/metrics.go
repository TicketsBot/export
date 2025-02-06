package middleware

import (
	"github.com/TicketsBot/export/internal/metrics"
	"github.com/go-chi/chi/v5"
	"net/http"
)

func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		metrics.ApiRequests.WithLabelValues(r.Method, route).Inc()
	})
}
