# GoTinyFugue (gtf)
# the version is the VERSION file, embedded at compile time
VERSION ?= $(shell cat VERSION)
LDFLAGS := -s -w

.PHONY: all build run test vet fmt wiki helpcheck dist dist-all packages clean

all: build

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o gtf ./cmd/gtf

# go run, as a development build
run:
	go run ./cmd/gtf

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal gtflib help

# validate the help topics and their links
helpcheck:
	go run ./cmd/helpwiki -check

# build the GitHub wiki pages from the help topics into build/wiki (publishing is a separate step: help/README.md)
wiki:
	go run ./cmd/helpwiki -out build/wiki

# The release tarball for this machine: dist/gtf-VERSION-<os>-<arch>.tar.gz (+ SHA256SUMS)
dist:
	@mkdir -p dist
	@./scripts/dist.sh "$(VERSION)" "$$(go env GOOS)" "$$(go env GOARCH)"
	@./scripts/dist.sh --sums

# Every platform the installer supports.
dist-all:
	@mkdir -p dist
	@set -e; for t in linux/amd64 linux/arm64 linux/386 linux/arm/7 freebsd/amd64 freebsd/arm64 darwin/amd64 darwin/arm64; do \
	  os=$${t%%/*}; rest=$${t#*/}; arch=$${rest%%/*}; arm=""; case "$$rest" in */*) arm=$${rest#*/};; esac; \
	  ./scripts/dist.sh "$(VERSION)" "$$os" "$$arch" "$$arm"; \
	done
	@./scripts/dist.sh --sums

# .deb and .rpm for this machine's Linux architecture (needs nfpm or network for `go run`)
packages:
	@mkdir -p dist
	@./scripts/package.sh "$(VERSION)" "$$(go env GOARCH)" "$$(go env GOARM)"
	@./scripts/dist.sh --sums

clean:
	rm -rf dist gtf
