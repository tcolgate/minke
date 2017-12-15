# Minke

Principles:

- Merge kubernetes Ingress
  - allows a single LB, and DNS entry to have paths served by
    different pods.
  - "unmerging" can be supported by selector and ingress classes.
- Stick to the "Spec"
  - IngressController behaviour is a bit vague, but we go by the word of the
    spec. Paths are POSIX regex, anchored at the start, open at the end.
    - An annotation may be provided to support plain prefix matching if
      a considerable performance benefit can be proven
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

# Igress Aggregation

The specification for kubernetes ingresses is vague on specific
implementation details.

- Ingresses with explicit http hostnames set are collected together. They are then:
  * sorted by any "minke..../priority" annotation, in ascending order
    (Priority 1 rules are process before priority 2).
  * Those with no priority set are considered be be of the lowest priority and rules
    for them are handled last.
  * Those of the same priority are sorted alphabetically by name then namespace
- Ingresses with no HTTP Hostname set are sorted as above.

Incoming traffic is then processed as follows:

- If a hostname matches, the set of Ingresses for that hostname as selected.
  - Ingresses are processed in the sorted order as specific above.
    - The rules for each ingress are process in the order from the specification
      - If a match is found the matching ingress backend is selected.
    - If no rules match, the set of ingresses is searched for a default
      backend. The first default backend is selected.
- If no hostname matches or no backend was selected by the hostname rule sets, the
  set of rules with no hostname are selected, and are processed as above.
- If no backend is selected a 502 response is returned to the client.

Once a backend is selected the set of associated endpointed are queried.

- At present the only selection strategy is random.

