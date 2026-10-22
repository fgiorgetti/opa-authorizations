# Stage 1: build
FROM registry.redhat.io/ubi9/go-toolset:latest AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o controller ./cmd/controller

# Stage 2: runtime
FROM registry.redhat.io/ubi9/ubi-minimal:latest

RUN microdnf install -y shadow-utils && microdnf clean all

RUN useradd -u 1001 -r -g 0 -s /sbin/nologin appuser

COPY --from=builder /build/controller /usr/local/bin/controller

USER 1001

EXPOSE 8080

CMD ["/usr/local/bin/controller"]
