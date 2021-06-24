.PHONY: all binary docs docs-in-container build-local clean install install-binary install-completions shell test-integration .install.vndr vendor vendor-in-container

export GOPROXY=https://proxy.golang.org

# On some plaforms (eg. macOS, FreeBSD) gpgme is installed in /usr/local/ but /usr/local/include/ is
# not in the default search path. Rather than hard-code this directory, use gpgme-config.
# Sadly that must be done at the top-level user instead of locally in the gpgme subpackage, because cgo
# supports only pkg-config, not general shell scripts, and gpgme does not install a pkg-config file.
# If gpgme is not installed or gpgme-config can’t be found for other reasons, the error is silently ignored
# (and the user will probably find out because the cgo compilation will fail).
GPGME_ENV := CGO_CFLAGS="$(shell gpgme-config --cflags 2>/dev/null)" CGO_LDFLAGS="$(shell gpgme-config --libs 2>/dev/null)"

# The following variables very roughly follow https://www.gnu.org/prep/standards/standards.html#Makefile-Conventions .
DESTDIR ?=
PREFIX ?= /usr/local
CONTAINERSCONFDIR ?= /etc/containers
REGISTRIESDDIR ?= ${CONTAINERSCONFDIR}/registries.d
SIGSTOREDIR ?= /var/lib/containers/sigstore
BINDIR ?= ${PREFIX}/bin
MANDIR ?= ${PREFIX}/share/man
BASHCOMPLETIONSDIR ?= ${PREFIX}/share/bash-completion/completions

GO ?= go
GOBIN := $(shell $(GO) env GOBIN)
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ifeq ($(GOBIN),)
GOBIN := $(GOPATH)/bin
endif

# Multiple scripts are sensitive to this value, make sure it's exported/available
# N/B: Need to use 'command -v' here for compatibility with MacOS.
export CONTAINER_RUNTIME ?= $(if $(shell command -v podman),podman,docker)
GOMD2MAN ?= $(if $(shell command -v go-md2man),go-md2man,$(GOBIN)/go-md2man)

# Go module support: set `-mod=vendor` to use the vendored sources.
# See also hack/make.sh.
ifeq ($(shell go help mod >/dev/null 2>&1 && echo true), true)
  GO:=GO111MODULE=on $(GO)
  MOD_VENDOR=-mod=vendor
endif

ifeq ($(DEBUG), 1)
  override GOGCFLAGS += -N -l
endif

ifeq ($(GOOS), linux)
  ifneq ($(GOARCH),$(filter $(GOARCH),mips mipsle mips64 mips64le ppc64 riscv64))
    GO_DYN_FLAGS="-buildmode=pie"
  endif
endif

GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null)

# If $TESTFLAGS is set, it is passed as extra arguments to 'go test'.
# You can increase test outputerbosity with the option '-test.vv'.
# You can select certain tests to run, with `-test.run <regex>` for example:
#
#     make test-unit TESTFLAGS='-test.run ^TestBuild$'
#
# For integration test, we use [gocheck](https://labix.org/gocheck).
# You can increase test outputerbosity with the option '-check.vv'.
# You can limit test selection with `-check.f <regex>`, for example:
#
#     make test-integration TESTFLAGS='-check.f DockerSuite.TestBuild*'
#
export TESTFLAGS ?= -v -check.v -test.timeout=15m

# This is assumed to be set non-empty when operating inside a CI/automation environment
CI ?=

# This env. var. is interpreted by some tests as a permission to
# modify local configuration files and services.
export SKOPEO_CONTAINER_TESTS ?= $(if $(CI),1,0)

# This is a compromise, we either use a container for this or require
# the local user to have a compatible python3 development environment.
# Define it as a "resolve on use" variable to avoid calling out when possible
export SKOPEO_CIDEV_CONTAINER_FQIN ?= $(shell hack/get_fqin.sh)
CONTAINER_CMD ?= ${CONTAINER_RUNTIME} run --rm -i -e TESTFLAGS="$(TESTFLAGS)" -e CI=$(CI)
# if this session isn't interactive, then we don't want to allocate a
# TTY, which would fail, but if it is interactive, we do want to attach
# so that the user can send e.g. ^C through.
INTERACTIVE := $(shell [ -t 0 ] && echo 1 || echo 0)
ifeq ($(INTERACTIVE), 1)
	CONTAINER_CMD += -t
endif
CONTAINER_GOSRC = /src/github.com/containers/skopeo
CONTAINER_RUN ?= $(CONTAINER_CMD) --security-opt label=disable -v $(CURDIR):$(CONTAINER_GOSRC) -w $(CONTAINER_GOSRC) -e SKOPEO_CONTAINER_TESTS=$(SKOPEO_CONTAINER_TESTS) $(SKOPEO_CIDEV_CONTAINER_FQIN)

GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null || true)

EXTRA_LDFLAGS ?=
SKOPEO_LDFLAGS := -ldflags '-X main.gitCommit=${GIT_COMMIT} $(EXTRA_LDFLAGS)'

MANPAGES_MD = $(wildcard docs/*.md)
MANPAGES ?= $(MANPAGES_MD:%.md=%)

BTRFS_BUILD_TAG = $(shell hack/btrfs_tag.sh) $(shell hack/btrfs_installed_tag.sh)
LIBDM_BUILD_TAG = $(shell hack/libdm_tag.sh)
LOCAL_BUILD_TAGS = $(BTRFS_BUILD_TAG) $(LIBDM_BUILD_TAG)
BUILDTAGS += $(LOCAL_BUILD_TAGS)

ifeq ($(DISABLE_CGO), 1)
	override BUILDTAGS = exclude_graphdriver_devicemapper exclude_graphdriver_btrfs containers_image_openpgp
endif

#   make all DEBUG=1
#     Note: Uses the -N -l go compiler options to disable compiler optimizations
#           and inlining. Using these build options allows you to subsequently
#           use source debugging tools like delve.
all: bin/skopeo docs

help:
	@echo "Usage: make <target>"
	@echo
	@echo "Defaults to building bin/skopeo and docs"
	@echo
	@echo " * 'install' - Install binaries and documents to system locations"
	@echo " * 'binary' - Build skopeo with a container"
	@echo " * 'static' - Build statically linked binary"
	@echo " * 'bin/skopeo' - Build skopeo locally"
	@echo " * 'test-unit' - Execute unit tests"
	@echo " * 'test-integration' - Execute integration tests"
	@echo " * 'validate' - Verify whether there is no conflict and all Go source files have been formatted, linted and vetted"
	@echo " * 'check' - Including above validate, test-integration and test-unit"
	@echo " * 'shell' - Run the built image and attach to a shell"
	@echo " * 'clean' - Clean artifacts"

# Do the build and the output (skopeo) should appear in current dir
binary: cmd/skopeo
	$(CONTAINER_RUN) make bin/skopeo $(if $(DEBUG),DEBUG=$(DEBUG)) BUILDTAGS='$(BUILDTAGS)'

# Update nix/nixpkgs.json its latest stable commit
.PHONY: nixpkgs
nixpkgs:
	@nix run \
		-f channel:nixos-21.05 nix-prefetch-git \
		-c nix-prefetch-git \
		--no-deepClone \
		https://github.com/nixos/nixpkgs refs/heads/nixos-21.05 > nix/nixpkgs.json

# Build statically linked binary
.PHONY: static
static:
	@nix build -f nix/
	mkdir -p ./bin
	cp -rfp ./result/bin/* ./bin/

# Build w/o using containers
.PHONY: bin/skopeo
bin/skopeo:
	$(GPGME_ENV) $(GO) build $(MOD_VENDOR) ${GO_DYN_FLAGS} ${SKOPEO_LDFLAGS} -gcflags "$(GOGCFLAGS)" -tags "$(BUILDTAGS)" -o $@ ./cmd/skopeo
bin/skopeo.%:
	GOOS=$(word 2,$(subst ., ,$@)) GOARCH=$(word 3,$(subst ., ,$@)) $(GO) build $(MOD_VENDOR) ${SKOPEO_LDFLAGS} -tags "containers_image_openpgp $(BUILDTAGS)" -o $@ ./cmd/skopeo
local-cross: bin/skopeo.darwin.amd64 bin/skopeo.linux.arm bin/skopeo.linux.arm64 bin/skopeo.windows.386.exe bin/skopeo.windows.amd64.exe

$(MANPAGES): %: %.md
	sed -e 's/\((skopeo.*\.md)\)//' -e 's/\[\(skopeo.*\)\]/\1/' $<  | $(GOMD2MAN) -in /dev/stdin -out $@

docs: $(MANPAGES)

docs-in-container:
	${CONTAINER_RUN} $(MAKE) docs $(if $(DEBUG),DEBUG=$(DEBUG))

clean:
	rm -rf bin docs/*.1

install: install-binary install-docs install-completions
	install -d -m 755 ${DESTDIR}${SIGSTOREDIR}
	install -d -m 755 ${DESTDIR}${CONTAINERSCONFDIR}
	install -m 644 default-policy.json ${DESTDIR}${CONTAINERSCONFDIR}/policy.json
	install -d -m 755 ${DESTDIR}${REGISTRIESDDIR}
	install -m 644 default.yaml ${DESTDIR}${REGISTRIESDDIR}/default.yaml

install-binary: bin/skopeo
	install -d -m 755 ${DESTDIR}${BINDIR}
	install -m 755 bin/skopeo ${DESTDIR}${BINDIR}/skopeo

install-docs: docs
	install -d -m 755 ${DESTDIR}${MANDIR}/man1
	install -m 644 docs/*.1 ${DESTDIR}${MANDIR}/man1

install-completions:
	install -m 755 -d ${DESTDIR}${BASHCOMPLETIONSDIR}
	install -m 644 completions/bash/skopeo ${DESTDIR}${BASHCOMPLETIONSDIR}/skopeo

shell:
	$(CONTAINER_RUN) bash

check: validate test-unit test-integration test-system

test-integration:
	$(CONTAINER_RUN) $(MAKE) test-integration-local SKOPEO_CONTAINER_TESTS=1


# Intended for CI, assumed to be running in quay.io/libpod/skopeo_cidev container.
test-integration-local: bin/skopeo
	hack/make.sh test-integration

# complicated set of options needed to run podman-in-podman
test-system:
	DTEMP=$(shell mktemp -d --tmpdir=/var/tmp podman-tmp.XXXXXX); \
	$(CONTAINER_CMD) --privileged \
		-e SKOPEO_CONTAINER_TESTS=1 \
		-v $(CURDIR):$(CONTAINER_GOSRC) -w $(CONTAINER_GOSRC) \
		-v $$DTEMP:/var/lib/containers:Z -v /run/systemd/journal/socket:/run/systemd/journal/socket \
		"$(SKOPEO_CIDEV_CONTAINER_FQIN)" \
			$(MAKE) test-system-local; \
	rc=$$?; \
	$(RM) -rf $$DTEMP; \
	exit $$rc

# Intended for CI, assumed to already be running in quay.io/libpod/skopeo_cidev container.
test-system-local: bin/skopeo
	hack/make.sh test-system

test-unit:
	# Just call (make test unit-local) here instead of worrying about environment differences
	$(CONTAINER_RUN) $(MAKE) test-unit-local

validate:
	$(CONTAINER_RUN) $(MAKE) validate-local

# This target is only intended for development, e.g. executing it from an IDE. Use (make test) for CI or pre-release testing.
test-all-local: validate-local validate-docs test-unit-local

.PHONY: validate-local
validate-local:
	hack/make.sh validate-git-marks validate-gofmt validate-lint validate-vet

# This invokes bin/skopeo, hence cannot be run as part of validate-local
.PHONY: validate-docs
validate-docs:
	hack/man-page-checker
	hack/xref-helpmsgs-manpages

test-unit-local: bin/skopeo
	$(GPGME_ENV) $(GO) test $(MOD_VENDOR) -tags "$(BUILDTAGS)" $$($(GO) list $(MOD_VENDOR) -tags "$(BUILDTAGS)" -e ./... | grep -v '^github\.com/containers/skopeo/\(integration\|vendor/.*\)$$')

vendor:
	$(GO) mod tidy
	$(GO) mod vendor
	$(GO) mod verify

vendor-in-container:
	podman run --privileged --rm --env HOME=/root -v $(CURDIR):/src -w /src quay.io/libpod/golang:1.16 $(MAKE) vendor
