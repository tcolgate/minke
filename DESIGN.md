# Minke

Principles:
- Merge kubernetes Ingress
  - allows a single LB, and DNS entry to have paths served by
    different pods.
  - "unmerging" can be supported by selector and ingress classes.
- Stick to the "Spec"
  - IngressController behaviour is a bit vague, but we go by the word of the
    spec. Paths are POSIX regex, anchored at the start, open at the end.
  - Within an Ingress object, first match wins.
  - Defaults Backends are only used if no Ingresses have a direct Path match
  - When multiple Ingress paths match, or multiple 
    default Backends are hit, we will loadbalance resources
    across them. Possibly support a selection by some Priority.
- Minimize features.
  - If something can be achieved by running multiple controllers,
    e.g. http vs https handling, blocking, http redirect, we will not provide
    magic to do it for you.
  - Rely on http.ReverseProxy with two exceptions.
    - Support websocket hijack.
    - Support h2c http2.
- Maximize observability.
  - Out of the box OpenTracing support (at least jaeger, probably zipkin).
  - Out of the box prometheus metrics.
  - Enable debug endpoints on the administrative endpoint.
