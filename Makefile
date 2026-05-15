VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS    := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: build build-dansal build-web build-dansal_admin run clean

build: build-dansal build-web build-dansal_admin

build-dansal:
	go build $(LDFLAGS) -o dansal ./cmd/dansal

build-web:
	go build -o web ./cmd/web

build-dansal_admin:
	go build -o dansal_admin ./cmd/dansal_admin

run: build-dansal
	./dansal

clean:
	rm -f dansal web dansal_admin
