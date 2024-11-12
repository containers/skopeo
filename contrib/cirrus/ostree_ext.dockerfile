ARG BASE_FQIN=quay.io/coreos-assembler/fcos-buildroot:testing-devel
FROM $BASE_FQIN

# See 'Danger of using COPY and ADD instructions'
# at https://cirrus-ci.org/guide/docker-builder-vm/#dockerfile-as-a-ci-environment
# Provide easy way to force-invalidate image cache by .cirrus.yml change
ARG CIRRUS_IMAGE_VERSION
ENV CIRRUS_IMAGE_VERSION=$CIRRUS_IMAGE_VERSION
ADD https://sh.rustup.rs /var/tmp/rustup_installer.sh

RUN dnf remove -y rust && \
    chmod +x /var/tmp/rustup_installer.sh && \
    /var/tmp/rustup_installer.sh -y --default-toolchain stable --profile minimal

ENV PATH=/root/.cargo/bin:/root/.local/bin:/root/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
