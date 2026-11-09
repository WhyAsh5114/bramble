# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
COPY *.go ./
COPY relaysrv ./relaysrv
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /relay . \
 && mkdir -p /out/state

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /relay /relay
# A new named volume copies this directory, owner included, so the nonroot user can write its key.
COPY --from=build --chown=65532:65532 /out/state /state
ENV RELAY_STATE_DIR=/state
VOLUME /state
EXPOSE 9420/tcp 3478/udp 40000-40999/udp
USER 65532:65532
ENTRYPOINT ["/relay"]
