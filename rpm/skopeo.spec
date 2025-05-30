%global with_debug 1

%if 0%{?with_debug}
%global _find_debuginfo_dwz_opts %{nil}
%global _dwz_low_mem_die_limit 0
%else
%global debug_package %{nil}
%endif

%global gomodulesmode GO111MODULE=on

# No btrfs on RHEL
%if %{defined fedora}
%define build_with_btrfs 1
%endif

%if %{defined rhel}
%define fips 1
%endif

# Only used in official koji builds
# Copr builds set a separate epoch for all environments
%if %{defined fedora}
%define conditional_epoch 1
%define fakeroot 1
%else
%define conditional_epoch 2
%endif

Name: skopeo
%if %{defined copr_username}
Epoch: 102
%else
Epoch: %{conditional_epoch}
%endif
# DO NOT TOUCH the Version string!
# The TRUE source of this specfile is:
# https://github.com/containers/skopeo/blob/main/rpm/skopeo.spec
# If that's what you're reading, Version must be 0, and will be updated by Packit for
# copr and koji builds.
# If you're reading this on dist-git, the version is automatically filled in by Packit.
Version: 0
# The `AND` needs to be uppercase in the License for SPDX compatibility
License: Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND ISC AND MIT AND MPL-2.0
Release: %autorelease
%if %{defined golang_arches_future}
ExclusiveArch: %{golang_arches_future}
%else
ExclusiveArch: aarch64 ppc64le s390x x86_64
%endif
Summary: Inspect container images and repositories on registries
URL: https://github.com/containers/%{name}
# Tarball fetched from upstream
Source0: %{url}/archive/v%{version}.tar.gz
BuildRequires: %{_bindir}/go-md2man
%if %{defined build_with_btrfs}
BuildRequires: btrfs-progs-devel
%endif
BuildRequires: git-core
BuildRequires: golang
%if !%{defined gobuild}
BuildRequires: go-rpm-macros
%endif
BuildRequires: gpgme-devel
BuildRequires: libassuan-devel
BuildRequires: glib2-devel
BuildRequires: make
BuildRequires: shadow-utils-subid-devel
Requires: containers-common >= 4:1-21

%description
Command line utility to inspect images and repositories directly on Docker
registries without the need to pull them

# NOTE: The tests subpackage is only intended for testing and will not be supported
# for end-users and/or customers.
%package tests
Summary: Tests for %{name}

Requires: %{name} = %{epoch}:%{version}-%{release}
Requires: bats
%if %{defined fakeroot}
Requires: fakeroot
%endif
Requires: gnupg
Requires: jq
Requires: golang
Requires: podman
Requires: crun
Requires: httpd-tools
Requires: openssl
Requires: squashfs-tools

%description tests
%{summary}

This package contains system tests for %{name}

%prep
%autosetup -Sgit %{name}-%{version}
# The %%install stage should not rebuild anything but only install what's
# built in the %%build stage. So, remove any dependency on build targets.
sed -i 's/^install-binary: bin\/%{name}.*/install-binary:/' Makefile
sed -i 's/^completions: bin\/%{name}.*/completions:/' Makefile
sed -i 's/^install-docs: docs.*/install-docs:/' Makefile

%build
%set_build_flags
export CGO_CFLAGS=$CFLAGS

# These extra flags present in $CFLAGS have been skipped for now as they break the build
CGO_CFLAGS=$(echo $CGO_CFLAGS | sed 's/-flto=auto//g')
CGO_CFLAGS=$(echo $CGO_CFLAGS | sed 's/-Wp,D_GLIBCXX_ASSERTIONS//g')
CGO_CFLAGS=$(echo $CGO_CFLAGS | sed 's/-specs=\/usr\/lib\/rpm\/redhat\/redhat-annobin-cc1//g')

%ifarch x86_64
export CGO_CFLAGS="$CGO_CFLAGS -m64 -mtune=generic -fcf-protection=full"
%endif

BASEBUILDTAGS="$(hack/libsubid_tag.sh)"
%if %{defined build_with_btrfs}
export BUILDTAGS="$BASEBUILDTAGS $(hack/btrfs_installed_tag.sh)"
%else
export BUILDTAGS="$BASEBUILDTAGS exclude_graphdriver_btrfs"
%endif

%if %{defined fips}
export BUILDTAGS="$BUILDTAGS libtrust_openssl"
%endif

# unset LDFLAGS earlier set from set_build_flags
LDFLAGS=''

%gobuild -o bin/%{name} ./cmd/%{name}
%{__make} docs

%install
make \
    DESTDIR=%{buildroot} \
    PREFIX=%{_prefix} \
    install-binary install-docs install-completions

# system tests
install -d -p %{buildroot}/%{_datadir}/%{name}/test/system
cp -pav systemtest/* %{buildroot}/%{_datadir}/%{name}/test/system/

#define license tag if not already defined
%{!?_licensedir:%global license %doc}

# Include this to silence rpmlint.
# Especially annoying if you use syntastic vim plugin.
%check

%files
%license LICENSE
%doc README.md
%{_bindir}/%{name}
%{_mandir}/man1/%{name}*
%dir %{_datadir}/bash-completion
%dir %{_datadir}/bash-completion/completions
%{_datadir}/bash-completion/completions/%{name}
%dir %{_datadir}/fish/vendor_completions.d
%{_datadir}/fish/vendor_completions.d/%{name}.fish
%dir %{_datadir}/zsh/site-functions
%{_datadir}/zsh/site-functions/_%{name}

%files tests
%license LICENSE vendor/modules.txt
%{_datadir}/%{name}/test

%changelog
%autochangelog
