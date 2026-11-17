# The S3 ACL gateway (cmd/s3gateway), built by FrankenManager from its
# embedded sources: standard library only, so no module download.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY . .
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=mod
RUN go build -trimpath -ldflags="-s -w" -o /s3gateway ./cmd/s3gateway

FROM scratch
COPY --from=build /s3gateway /s3gateway
ENTRYPOINT ["/s3gateway"]
