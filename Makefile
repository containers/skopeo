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

# N/B: This value is managed by Renovate, manual changes are
# possible, as long as they don't disturb the formatting
# (i.e. DO NOT ADD A 'v' prefix!)
GOLANGCI_LINT_VERSION := 2.2.1

ifeq ($(GOBIN),)
GOBIN := $(GOPATH)/bin
endif

# Scripts may also use CONTAINER_RUNTIME, so we need to export it.
# Note possibly non-obvious aspects of this:
# - We need to use 'command -v' here, not 'which', for compatibility with MacOS.
# - GNU Make 4.2.1 (included in Ubuntu 20.04) incorrectly tries to avoid invoking
#   a shell, and fails because there is no /usr/bin/command. The trailing ';' in
#   $(shell … ;) defeats that heuristic (recommended in
#   https://savannah.gnu.org/bugs/index.php?57625 ).
export CONTAINER_RUNTIME ?= $(if $(shell command -v podman ;),podman,docker)
GOMD2MAN ?= $(if $(shell command -v go-md2man ;),go-md2man,$(GOBIN)/go-md2man)

ifeq ($(DEBUG), 1)
  override GOGCFLAGS += -N -l
endif

ifeq ($(GOOS), linux)
  ifneq ($(GOARCH),$(filter $(GOARCH),mips mipsle mips64 mips64le ppc64 riscv64))
    GO_DYN_FLAGS="-buildmode=pie"
  endif
endif

# If $TESTFLAGS is set, it is passed as extra arguments to 'go test'.
# You can select certain tests to run, with `-run <regex>` for example:
#
#     make test-unit TESTFLAGS='-run ^TestManifestDigest$'
#     make test-integration TESTFLAGS='-run copySuite.TestCopy.*'
export TESTFLAGS ?= -timeout=15m

# This is assumed to be set non-empty when operating inside a CI/automation environment
CI ?=

# This env. var. is interpreted by some tests as a permission to
# modify local configuration files and services.
export SKOPEO_CONTAINER_TESTS ?= $(if $(CI),1,0)

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

GIT_COMMIT := $(shell GIT_CEILING_DIRECTORIES=$$(cd ..; pwd) git rev-parse HEAD 2> /dev/null || true)

EXTRA_LDFLAGS ?=
SKOPEO_LDFLAGS := -ldflags '-X main.gitCommit=${GIT_COMMIT} $(EXTRA_LDFLAGS)'

MANPAGES_MD = $(wildcard docs/*.md)
MANPAGES ?= $(MANPAGES_MD:%.md=%)

BTRFS_BUILD_TAG = $(shell hack/btrfs_installed_tag.sh)
LIBSUBID_BUILD_TAG = $(shell hack/libsubid_tag.sh)
SQLITE_BUILD_TAG = $(shell hack/sqlite_tag.sh)
LOCAL_BUILD_TAGS = $(BTRFS_BUILD_TAG) $(LIBSUBID_BUILD_TAG) $(SQLITE_BUILD_TAG)
BUILDTAGS += $(LOCAL_BUILD_TAGS)

ifeq ($(DISABLE_CGO), 1)
	override BUILDTAGS = exclude_graphdriver_btrfs containers_image_openpgp
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
	@echo " * 'bin/skopeo.OS.ARCH' - Build skopeo for specific OS and ARCH"
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
	$(GO) build ${GO_DYN_FLAGS} ${SKOPEO_LDFLAGS} -gcflags "$(GOGCFLAGS)" -tags "$(BUILDTAGS)" -o $@ ./cmd/skopeo
bin/skopeo.%:
	GOOS=$(word 2,$(subst ., ,$@)) GOARCH=$(word 3,$(subst ., ,$@)) $(GO) build ${SKOPEO_LDFLAGS} -tags "containers_image_openpgp $(BUILDTAGS)" -o $@ ./cmd/skopeo
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

tools:
	if [ ! -x "$(GOBIN)/golangci-lint" ]; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOBIN) v$(GOLANGCI_LINT_VERSION) ; \
	fi

check: validate test-unit test-integration test-system

test-integration:
# This is intended to be equal to $(CONTAINER_RUN), but with --cap-add=cap_mknod.
# --cap-add=cap_mknod is important to allow skopeo to use containers-storage: directly as it exists in the callers’ environment, without
# creating a nested user namespace (which requires /etc/subuid and /etc/subgid to be set up)
	$(CONTAINER_CMD) --security-opt label=disable --cap-add=cap_mknod -v $(CURDIR):$(CONTAINER_GOSRC) -w $(CONTAINER_GOSRC) $(SKOPEO_CIDEV_CONTAINER_FQIN) \
		$(MAKE) test-integration-local


# Intended for CI, assumed to be running in quay.io/libpod/skopeo_cidev container.
test-integration-local: bin/skopeo
	hack/warn-destructive-tests.sh
	hack/test-integration.sh

# complicated set of options needed to run podman-in-podman
test-system:
	DTEMP=$(shell mktemp -d --tmpdir=/var/tmp podman-tmp.XXXXXX); \
	$(CONTAINER_CMD) --privileged \
		-v $(CURDIR):$(CONTAINER_GOSRC) -w $(CONTAINER_GOSRC) \
		-v $$DTEMP:/var/lib/containers:Z -v /run/systemd/journal/socket:/run/systemd/journal/socket \
		"$(SKOPEO_CIDEV_CONTAINER_FQIN)" \
			$(MAKE) test-system-local; \
	rc=$$?; \
	$(CONTAINER_RUNTIME) unshare rm -rf $$DTEMP; # This probably doesn't work with Docker, oh well, better than nothing... \
	exit $$rc

# Intended for CI, assumed to already be running in quay.io/libpod/skopeo_cidev container.
test-system-local: bin/skopeo
	hack/warn-destructive-tests.sh
	hack/test-system.sh

test-unit:
	# Just call (make test unit-local) here instead of worrying about environment differences
	$(CONTAINER_RUN) $(MAKE) test-unit-local

validate:
	$(CONTAINER_RUN) $(MAKE) validate-local

# This target is only intended for development, e.g. executing it from an IDE. Use (make test) for CI or pre-release testing.
test-all-local: validate-local validate-docs test-unit-local

.PHONY: validate-local
validate-local:
	hack/validate-git-marks.sh
	hack/validate-gofmt.sh
	$(GOBIN)/golangci-lint run --build-tags "${BUILDTAGS}"
	# An extra run with --tests=false allows detecting code unused outside of tests;
	# ideally the linter should be able to find this automatically.
	# Since everything is already cached, this additional run doesn't take much time.
	$(GOBIN)/golangci-lint run --build-tags "${BUILDTAGS}" --tests=false
	BUILDTAGS="${BUILDTAGS}" hack/validate-vet.sh

# This invokes bin/skopeo, hence cannot be run as part of validate-local
.PHONY: validate-docs
validate-docs: bin/skopeo
	hack/man-page-checker
	hack/xref-helpmsgs-manpages

test-unit-local:
	$(GO) test -tags "$(BUILDTAGS)" $$($(GO) list -tags "$(BUILDTAGS)" -e ./... | grep -v '^github\.com/containers/skopeo/\(integration\|vendor/.*\)$$')

vendor:
	$(GO) mod tidy
	$(GO) mod vendor
	$(GO) mod verify

vendor-in-container:
	podman run --privileged --rm --env HOME=/root -v $(CURDIR):/src -w /src golang $(MAKE) vendor

# CAUTION: This is not a replacement for RPMs provided by your distro.
# Only intended to build and test the latest unreleased changes.
rpm:
	rpkg local
