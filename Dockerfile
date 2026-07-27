# syntax=docker/dockerfile:1.7

FROM public.ecr.aws/docker/library/golang:1.25.0-alpine3.22 AS build

ARG VERSION=development
ARG GOPROXY=https://proxy.golang.org,direct
ARG GO_BUILD_PARALLELISM=2
ARG GOMAXPROCS=2

ENV GOPROXY=${GOPROXY}
ENV GOMAXPROCS=${GOMAXPROCS}

WORKDIR /Activity-Relay

RUN apk add --no-cache git

COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . /Activity-Relay

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    mkdir -p /rootfs/usr/bin && \
    go build \
      -p "${GO_BUILD_PARALLELISM}" \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /rootfs/usr/bin/relay \
      .

FROM public.ecr.aws/docker/library/alpine:3.22.1

COPY --from=build /rootfs/usr/bin/relay /usr/bin/relay
COPY --from=build /Activity-Relay/contrib/web /usr/share/activity-relay/web
COPY --from=build /Activity-Relay/contrib/ops/resource-guard.py /usr/lib/activity-relay/resource-guard.py
COPY --from=build /Activity-Relay/contrib/ops/activity-relay-resource-guard /usr/bin/activity-relay-resource-guard

RUN chmod 0755 \
      /usr/bin/relay \
      /usr/bin/activity-relay-resource-guard \
      /usr/lib/activity-relay/resource-guard.py && \
    apk add --no-cache ca-certificates python3

ENTRYPOINT ["/usr/bin/relay"]
