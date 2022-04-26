.PHONY: all binary docs docs-in-container build-local clean install install-binary install-completions shell test-integration .install.vndr vendor vendor-in-container

export GOPROXY=https://proxy.golang.org

# The following variables very roughly follow https://www.gnu.org/prep/standards/standards.html#Makefile-Conventions .
DESTDIR ?=
PREFIX ?= /usr/local
ifeq ($(shell uname -s),FreeBSD)
CONTAINERSCONFDIR ?= /usr/local/etc/containers
else
CONTAINERSCONFDIR ?= /etc/containers
endif
REGISTRIESDDIR ?= ${CONTAINERSCONFDIR}/registries.d
LOOKASIDEDIR ?= /var/lib/containers/sigstore
BINDIR ?= ${PREFIX}/bin
MANDIR ?= ${PREFIX}/share/man

BASHINSTALLDIR=${PREFIX}/share/bash-completion/completions
ZSHINSTALLDIR=${PREFIX}/share/zsh/site-functions
FISHINSTALLDIR=${PREFIX}/share/fish/vendor_completions.d

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
export COMPOSE_CMD ?= $(if $(shell command -v podman-compose),podman-compose,docker-compose)
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
# You can increase test output verbosity with the option '-test.vv'.
# You can select certain tests to run, with `-test.run <regex>` for example:
#
#     make test-unit TESTFLAGS='-test.run ^TestManifestDigest$'
#
# For integration test, we use [gocheck](https://labix.org/gocheck).
# You can increase test output verbosity with the option '-check.vv'.
# You can limit test selection with `-check.f <regex>`, for example:
#
#     make test-integration TESTFLAGS='-check.f CopySuite.TestCopy.*'
export TESTFLAGS ?= -v -check.v -test.timeout=15m

# This is assumed to be set non-empty when operating inside a CI/automation environment
CI ?=

# This env. var. is interpreted by some tests as a permission to
# modify local configuration files and services.
export SKOPEO_CONTAINER_TESTS ?= $(if $(CI),1,0)

# The following env vars are only required for sigstore integration tests
# see hack/sigstore-setup
export VAULT_TOKEN ?= testtoken
export VAULT_ADDR ?= http://localhost:8200/
export AWS_ACCESS_KEY_ID ?= test
export AWS_SECRET_ACCESS_KEY ?= test
export AWS_REGION ?= us-east-1
export AWS_ENDPOINT ?= localhost:4566
export AWS_TLS_INSECURE_SKIP_VERIFY ?= 1
export OIDC_ISSUER ?= http://127.0.0.1:5556/auth
export OIDC_ID ?= sigstore

# This is a compromise, we either use a container for this or require
# the local user to have a compatible python3 development environment.
# Define it as a "resolve on use" variable to avoid calling out when possible
SKOPEO_CIDEV_CONTAINER_FQIN ?= $(shell hack/get_fqin.sh)
CONTAINER_CMD ?= ${CONTAINER_RUNTIME} run --rm -i -e TESTFLAGS="$(TESTFLAGS)" -e CI=$(CI) -e SKOPEO_CONTAINER_TESTS=1
# if this session isn't interactive, then we don't want to allocate a
# TTY, which would fail, but if it is interactive, we do want to attach
# so that the user can send e.g. ^C through.
INTERACTIVE := $(shell [ -t 0 ] && echo 1 || echo 0)
ifeq ($(INTERACTIVE), 1)
	CONTAINER_CMD += -t
endif
CONTAINER_GOSRC = /src/github.com/containers/skopeo
CONTAINER_RUN ?= $(CONTAINER_CMD) --security-opt label=disable -v $(CURDIR):$(CONTAINER_GOSRC) -w $(CONTAINER_GOSRC) $(SKOPEO_CIDEV_CONTAINER_FQIN)

GIT_COMMIT := $(shell git rev-parse HEAD 2> /dev/null || true)

EXTRA_LDFLAGS ?=
SKOPEO_LDFLAGS := -ldflags '-X main.gitCommit=${GIT_COMMIT} $(EXTRA_LDFLAGS)'

MANPAGES_MD = $(wildcard docs/*.md)
MANPAGES ?= $(MANPAGES_MD:%.md=%)

BTRFS_BUILD_TAG = $(shell hack/btrfs_tag.sh) $(shell hack/btrfs_installed_tag.sh)
LIBDM_BUILD_TAG = $(shell hack/libdm_tag.sh)
LIBSUBID_BUILD_TAG = $(shell hack/libsubid_tag.sh)
LOCAL_BUILD_TAGS = $(BTRFS_BUILD_TAG) $(LIBDM_BUILD_TAG) $(LIBSUBID_BUILD_TAG)
BUILDTAGS += $(LOCAL_BUILD_TAGS)

ifeq ($(DISABLE_CGO), 1)
	override BUILDTAGS = exclude_graphdriver_devicemapper exclude_graphdriver_btrfs containers_image_openpgp
endif

#   make all DEBUG=1
#     Note: Uses the -N -l go compiler options to disable compiler optimizations
#           and inlining. Using these build options allows you to subsequently
#           use source debugging tools like delve.
all: bin/skopeo docs

codespell:
	codespell -S Makefile,build,buildah,buildah.spec,imgtype,copy,AUTHORS,bin,vendor,.git,go.sum,CHANGELOG.md,changelog.txt,seccomp.json,.cirrus.yml,"*.xz,*.gz,*.tar,*.tgz,*ico,*.png,*.1,*.5,*.orig,*.rej" -L fpr,uint,iff,od,ERRO -w

help:
	@echo "Usage: make <target>"
	@echo
	@echo "Defaults to building bin/skopeo and docs"
	@echo
	@echo " * 'install' - Install binaries and documents to system locations"
	@echo " * 'binary' - Build skopeo with a container"
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

# Build w/o using containers
.PHONY: bin/skopeo
bin/skopeo:
	$(GO) build $(MOD_VENDOR) ${GO_DYN_FLAGS} ${SKOPEO_LDFLAGS} -gcflags "$(GOGCFLAGS)" -tags "$(BUILDTAGS)" -o $@ ./cmd/skopeo
bin/skopeo.%:
	GOOS=$(word 2,$(subst ., ,$@)) GOARCH=$(word 3,$(subst ., ,$@)) $(GO) build $(MOD_VENDOR) ${SKOPEO_LDFLAGS} -tags "containers_image_openpgp $(BUILDTAGS)" -o $@ ./cmd/skopeo
local-cross: bin/skopeo.darwin.amd64 bin/skopeo.linux.arm bin/skopeo.linux.arm64 bin/skopeo.windows.386.exe bin/skopeo.windows.amd64.exe

$(MANPAGES): %: %.md
ifneq ($(DISABLE_DOCS), 1)
	sed -e 's/\((skopeo.*\.md)\)//' -e 's/\[\(skopeo.*\)\]/\1/' $<  | $(GOMD2MAN) -in /dev/stdin -out $@
endif

docs: $(MANPAGES)

docs-in-container:
	${CONTAINER_RUN} $(MAKE) docs $(if $(DEBUG),DEBUG=$(DEBUG))

.PHONY: completions
completions: bin/skopeo
	install -d -m 755 completions/bash completions/zsh completions/fish completions/powershell
	./bin/skopeo completion bash >| completions/bash/skopeo
	./bin/skopeo completion zsh >| completions/zsh/_skopeo
	./bin/skopeo completion fish >| completions/fish/skopeo.fish
	./bin/skopeo completion powershell >| completions/powershell/skopeo.ps1

clean:
	rm -rf bin docs/*.1 completions/

install: install-binary install-docs install-completions
	install -d -m 755 ${DESTDIR}${LOOKASIDEDIR}
	install -d -m 755 ${DESTDIR}${CONTAINERSCONFDIR}
	install -m 644 default-policy.json ${DESTDIR}${CONTAINERSCONFDIR}/policy.json
	install -d -m 755 ${DESTDIR}${REGISTRIESDDIR}
	install -m 644 default.yaml ${DESTDIR}${REGISTRIESDDIR}/default.yaml

install-binary: bin/skopeo
	install -d -m 755 ${DESTDIR}${BINDIR}
	install -m 755 bin/skopeo ${DESTDIR}${BINDIR}/skopeo

install-docs: docs
ifneq ($(DISABLE_DOCS), 1)
	install -d -m 755 ${DESTDIR}${MANDIR}/man1
	install -m 644 docs/*.1 ${DESTDIR}${MANDIR}/man1
endif

install-completions: completions
	install -d -m 755 ${DESTDIR}${BASHINSTALLDIR}
	install -m 644 completions/bash/skopeo ${DESTDIR}${BASHINSTALLDIR}
	install -d -m 755 ${DESTDIR}${ZSHINSTALLDIR}
	install -m 644 completions/zsh/_skopeo ${DESTDIR}${ZSHINSTALLDIR}
	install -d -m 755 ${DESTDIR}${FISHINSTALLDIR}
	install -m 644 completions/fish/skopeo.fish ${DESTDIR}${FISHINSTALLDIR}
	# There is no common location for powershell files so do not install them. Users have to source the file from their powershell profile.

shell:
	$(CONTAINER_RUN) bash

check: validate test-unit test-integration test-system

test-integration:
	$(CONTAINER_RUN) $(MAKE) test-integration-local


# Intended for CI, assumed to be running in quay.io/libpod/skopeo_cidev container.
test-integration-local: bin/skopeo
	hack/make.sh test-integration

# complicated set of options needed to run podman-in-podman
# TODO: The $(RM) command will likely fail w/o `podman unshare`
test-system:
	DTEMP=$(shell mktemp -d --tmpdir=/var/tmp podman-tmp.XXXXXX); \
	$(CONTAINER_CMD) --privileged \
		-v $(CURDIR):$(CONTAINER_GOSRC) -w $(CONTAINER_GOSRC) \
		-v $$DTEMP:/var/lib/containers:Z -v /run/systemd/journal/socket:/run/systemd/journal/socket \
		"$(SKOPEO_CIDEV_CONTAINER_FQIN)" \
			$(MAKE) test-system-local; \
	rc=$$?; \
	-$(RM) -rf $$DTEMP; \
	exit $$rc

# Intended for CI, assumed to already be running in quay.io/libpod/skopeo_cidev container.
test-system-local: bin/skopeo
	hack/make.sh test-system

test-unit:
	# Just call (make test unit-local) here instead of worrying about environment differences
	$(CONTAINER_RUN) $(MAKE) test-unit-local

sigstore-testenv-up:
	cd hack/sigstore-setup && ./sigstore-setup.sh setup

sigstore-testenv-down:
	cd hack/sigstore-setup && ./sigstore-setup.sh cleanup

validate:
	$(CONTAINER_RUN) $(MAKE) validate-local

# This target is only intended for development, e.g. executing it from an IDE. Use (make test) for CI or pre-release testing.
test-all-local: validate-local validate-docs test-unit-local

.PHONY: validate-local
validate-local:
	BUILDTAGS="${BUILDTAGS}" hack/make.sh validate-git-marks validate-gofmt validate-lint validate-vet

# This invokes bin/skopeo, hence cannot be run as part of validate-local
.PHONY: validate-docs
validate-docs:
	hack/man-page-checker
	hack/xref-helpmsgs-manpages

test-unit-local: bin/skopeo
	$(GO) test $(MOD_VENDOR) -tags "$(BUILDTAGS)" $$($(GO) list $(MOD_VENDOR) -tags "$(BUILDTAGS)" -e ./... | grep -v '^github\.com/containers/skopeo/\(integration\|vendor/.*\)$$')

vendor:
	$(GO) mod tidy
	$(GO) mod vendor
	$(GO) mod verify

vendor-in-container:
	podman run --privileged --rm --env HOME=/root -v $(CURDIR):/src -w /src quay.io/libpod/golang:1.16 $(MAKE) vendor

# CAUTION: This is not a replacement for RPMs provided by your distro.
# Only intended to build and test the latest unreleased changes.
rpm:
	rpkg local
