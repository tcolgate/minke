package minke

import (
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

func addInboundTracing(ot trace.Tracer, next http.Handler) http.Handler {
	return next
}
