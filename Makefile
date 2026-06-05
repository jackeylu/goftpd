VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  = $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o goftpd.exe .

run:
	go run .

clean:
	rm -f goftpd.exe

# Bump patch version: 1.0.0 -> 1.0.1 (creates git tag only)
# No-op if HEAD already has a tag.
bump:
	@if git describe --tags --exact-match HEAD >/dev/null 2>&1; then \
		echo "Already tagged: $$(git describe --tags --exact-match HEAD)"; \
	else \
		v=$${v:-$$(git tag --sort=-version:refname | head -1)}; \
		v=$${v:-v0.0.0}; \
		v=$${v#v}; \
		major=$${v%%.*}; \
		rest=$${v#*.}; \
		minor=$${rest%%.*}; \
		patch=$${rest#*.}; \
		new="v$$major.$$minor.$$((patch+1))"; \
		git tag $$new && echo "Tagged $$new"; \
	fi

# Release: reuses tag if code unchanged, bumps if new commits.
# Idempotent — safe to run multiple times.
release:
	@tag=$$(git describe --tags --exact-match HEAD 2>/dev/null); \
	if [ -n "$$tag" ]; then \
		go build -ldflags "-X main.version=$$tag -X main.commit=$$(git rev-parse --short HEAD) -X main.date=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o goftpd.exe . && \
		echo "Rebuilt $$tag (no new commits)"; \
	else \
		v=$${v:-$$(git tag --sort=-version:refname | head -1)}; \
		v=$${v:-v0.0.0}; \
		v=$${v#v}; \
		major=$${v%%.*}; \
		rest=$${v#*.}; \
		minor=$${rest%%.*}; \
		patch=$${rest#*.}; \
		new="v$$major.$$minor.$$((patch+1))"; \
		git tag $$new && \
		go build -ldflags "-X main.version=$$new -X main.commit=$$(git rev-parse --short HEAD) -X main.date=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o goftpd.exe . && \
		echo "Released $$new"; \
	fi
