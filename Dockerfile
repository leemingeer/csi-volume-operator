# Build the manager binary
FROM golang:1.17 as builder
WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
ENV GOPROXY=https://goproxy.cn,direct
RUN go mod download
ENV GOPROXY=""

# Copy the go source
COPY cmd/direct-snapshot-rollback/main.go main.go
COPY api/ api/
COPY pkg/ pkg/

# Build GOARCH=amd64
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$ARCH go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM alpine:latest
WORKDIR /
COPY --from=builder /workspace/manager .
#USER nonroot:nonroot


ENTRYPOINT ["/manager"]
