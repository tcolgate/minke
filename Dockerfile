FROM golang:latest
RUN mkdir -p /go/src/github.com/tcolgate/minke
ADD . /go/src/github.com/tcolgate/minke/
WORKDIR /go/src/github.com/tcolgate/minke
RUN go build -o minke ./cmd/minke
ENTRYPOINT ["/go/src/github.com/tcolgate/minke/minke"]
