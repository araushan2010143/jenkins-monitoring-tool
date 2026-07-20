FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/observer ./cmd/observer
RUN CGO_ENABLED=0 go build -o /out/mockjenkins ./cmd/mockjenkins

FROM alpine:3.19 AS observer
RUN adduser -D -u 10001 appuser
COPY --from=build /out/observer /usr/local/bin/observer
USER appuser
ENTRYPOINT ["/usr/local/bin/observer"]

FROM alpine:3.19 AS mockjenkins
RUN adduser -D -u 10001 appuser
COPY --from=build /out/mockjenkins /usr/local/bin/mockjenkins
COPY --from=build /app/testdata/fixtures /testdata/fixtures
USER appuser
ENTRYPOINT ["/usr/local/bin/mockjenkins"]

FROM python:3.12-slim AS remediation
WORKDIR /app
RUN useradd -u 10001 -m appuser
COPY remediation/requirements.txt ./remediation/requirements.txt
RUN pip install --no-cache-dir -r remediation/requirements.txt
COPY remediation ./remediation
USER appuser
ENTRYPOINT ["python", "-m", "remediation.worker"]
