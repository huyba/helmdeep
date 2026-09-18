# Builds cmd/helmdeep-gateway as a static binary in a minimal image.
#
# As of Step 1 the binary is scaffold only (see ROADMAP.md) — this Dockerfile
# exists so the release pipeline and `make docker` have something real to
# build against from day one, not so there's a usable gateway image yet.

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/helmdeep-gateway ./cmd/helmdeep-gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/helmdeep-gateway /usr/local/bin/helmdeep-gateway
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/helmdeep-gateway"]
CMD ["serve"]
