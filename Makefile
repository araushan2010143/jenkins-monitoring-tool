GO_IMAGE := golang:1.25
GO_RUN := docker run --rm -v $(CURDIR):/app -w /app -e GOCACHE=/tmp/gocache -e GOFLAGS=-buildvcs=false $(GO_IMAGE)

PY_IMAGE := python:3.12-slim
PY_RUN := docker run --rm -v $(CURDIR):/app -w /app $(PY_IMAGE)

.PHONY: tidy build vet test test-go test-python up down logs

tidy:
	$(GO_RUN) go mod tidy

build:
	$(GO_RUN) go build ./...

vet:
	$(GO_RUN) go vet ./...

test: test-go test-python

test-go:
	$(GO_RUN) go test ./... -v

test-python:
	$(PY_RUN) sh -c "pip install --quiet --no-cache-dir -r remediation/requirements-dev.txt && python -m pytest remediation/tests -v"

up:
	docker compose up --build

down:
	docker compose down -v

logs:
	docker compose logs -f
