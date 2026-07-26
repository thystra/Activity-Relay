ARG GO_VERSION=1.25.0
FROM public.ecr.aws/docker/library/golang:${GO_VERSION}-alpine3.22 AS build

ARG VERSION=development
WORKDIR /Activity-Relay

COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /rootfs/usr/bin/relay \
    .

FROM public.ecr.aws/docker/library/alpine:3.22.1

COPY --from=build /rootfs/usr/bin/relay /usr/bin/relay
RUN chmod 0755 /usr/bin/relay && \
    apk add --no-cache ca-certificates tzdata

# Treat arguments supplied to `docker run` and Compose as relay subcommands.
ENTRYPOINT ["/usr/bin/relay"]

# A bare container invocation displays CLI help rather than failing silently.
CMD ["--help"]
