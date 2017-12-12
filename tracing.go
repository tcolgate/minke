package minke

import (
	"fmt"
	"net/http"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

func addInboundTracing(ot opentracing.Tracer, next http.Handler) http.Handler {
	opNameFunc := func(r *http.Request) string {
		return fmt.Sprintf("inbound %s", r.Host)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := ot.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
		span := ot.StartSpan(opNameFunc(r), ext.RPCServerOption(ctx))
		defer span.Finish()

		ext.Component.Set(span, "minke")
		ext.HTTPMethod.Set(span, r.Method)
		ext.HTTPUrl.Set(span, r.URL.String())
		span.SetTag("http.host", r.Host)
		ext.SpanKindRPCServer.Set(span)

		w = &statusCodeTracker{w, 200}
		r = r.WithContext(opentracing.ContextWithSpan(r.Context(), span))

		next.ServeHTTP(w, r)

		code := uint16(w.(*statusCodeTracker).status)
		ext.HTTPStatusCode.Set(span, code)
		if code >= 400 {
			ext.Error.Set(span, true)
		}
	})
}

type statusCodeTracker struct {
	http.ResponseWriter
	status int
}

func (s *statusCodeTracker) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

/*
func (s *statusCodeTracker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hi, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		errors.New("webserver doesn't support hijacking")
	}
	return hi.Hijack()
}

func (s *statusCodeTracker) CloseNotify() <-chan bool {
	cn := s.ResponseWriter.(http.CloseNotifier)
	return cn.CloseNotify()
}
*/
