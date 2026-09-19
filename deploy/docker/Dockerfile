# Builds cmd/helmdeep-gateway (the product) as a static binary in a minimal
# image. The quickstart also uses this same Dockerfile to build
# cmd/mock-upstream (a test fixture, not part of the product — see
# internal/mockupstream) by overriding BUILD_PKG, so the demo doesn't need a
# second Dockerfile to maintain.

ARG BUILD_PKG=./cmd/helmdeep-gateway

FROM golang:1.26-alpine AS build
ARG BUILD_PKG
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app "${BUILD_PKG}"
# distroless has no shell to mkdir/chown in the final stage, so create the
# gateway's writable data directory here (Alpine has a shell) and copy it
# in with the right ownership — the final image's nonroot user (65532)
# otherwise has no writable directory to put an audit log in at all.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /usr/local/bin/app
COPY --from=build --chown=nonroot:nonroot /out/data /data
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/app"]
CMD ["serve"]
