FROM public.ecr.aws/docker/library/golang:1.25.0-alpine3.22 AS build

ARG VERSION=development

WORKDIR /Activity-Relay
COPY . /Activity-Relay

RUN mkdir -p /rootfs/usr/bin && \
    apk add --no-cache git && \
    go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /rootfs/usr/bin/relay \
      .

FROM public.ecr.aws/docker/library/alpine:3.22.1

COPY --from=build /rootfs/usr/bin/relay /usr/bin/relay
COPY --from=build /Activity-Relay/contrib/web /usr/share/activity-relay/web

RUN chmod 0755 /usr/bin/relay && \
    apk add --no-cache ca-certificates

ENTRYPOINT ["/usr/bin/relay"]
