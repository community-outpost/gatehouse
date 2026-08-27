FROM --platform=$BUILDPLATFORM golang:1.25.13-alpine AS build

RUN apk add --no-cache ca-certificates
WORKDIR /src
ARG TARGETOS
ARG TARGETARCH
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/gatehouse ./cmd/gatehouse

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/gatehouse /gatehouse
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/gatehouse"]
