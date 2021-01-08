FROM golang:1.15-alpine AS build
RUN mkdir -p /src
ADD . /src
WORKDIR /src
RUN go build -o /minke ./cmd/minke
ENTRYPOINT ["/minke"]
FROM alpine:latest
COPY --from=build /minke /minke
ENTRYPOINT ["/minke"]
