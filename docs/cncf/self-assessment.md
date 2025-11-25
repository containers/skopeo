# Skopeo Self-assessment

## Table of contents

* [Metadata](#metadata)
  * [Security links](#security-links)
* [Overview](#overview)
  * [Actors](#actors)
  * [Actions](#actions)
  * [Background](#background)
  * [Goals](#goals)
  * [Non-goals](#non-goals)
* [Self-assessment use](#self-assessment-use)
* [Security functions and features](#security-functions-and-features)
* [Project compliance](#project-compliance)
* [Secure development practices](#secure-development-practices)
* [Security issue resolution](#security-issue-resolution)
* [Appendix](#appendix)

## Metadata

|||| | \-- | \-- | | Assessment Stage | Incomplete | | Software | [https://github.com/containers/skopeo](https://github.com/containers/skopeo) | | Security Provider | No | | Languages | Go | | SBOM | [https://github.com/containers/skopeo/blob/main/go.mod](https://github.com/containers/skopeo/blob/main/go.mod) |

### Security links

| Doc | url |
| :---- | :---- |
| Security file | [https://github.com/containers/skopeo/blob/main/SECURITY.md](https://github.com/containers/skopeo/blob/main/SECURITY.md) |

## Overview

Skopeo is a command-line utility that performs various operations on container images and image repositories. It provides tools for inspecting, copying, signing, and managing container images across different registries without requiring a container runtime or daemon.

### Background

Skopeo is a tool designed for working with remote container images and registries. Unlike traditional container tools, Skopeo operates without requiring a local container runtime, making it ideal for CI/CD pipelines and automated image management tasks.

Key characteristics:

- **Daemonless**: Operates without a container runtime or daemon
- **Registry operations**: Direct interaction with container registries
- **Image inspection**: Inspect images without downloading them
- **Image copying**: Copy images between registries efficiently
- **Image signing**: Support for signing and verifying image signatures
- **OCI-compliant**: Fully compatible with OCI standards
- **Multi-architecture**: Support for multi-architecture images

Skopeo is part of the containers ecosystem and integrates with other tools like Podman, Buildah, and CRI-O.

### Actors

* **Skopeo CLI**: The main command-line interface that users interact with for image operations.

* **Registry client**: Handles communication with container registries (Docker Hub, Quay.io, etc.).

* **Image store**: Manages local image storage when needed for operations.

* **Signature store**: Handles image signature storage and verification.

* **Credential manager**: Securely manages registry authentication credentials.

### Actions

* **Image inspection**:

  - Retrieves image metadata from registries
  - Inspects image configuration and layers
  - Examines image manifests without downloading
  - Verifies image integrity


* **Image copying**:

  - Copies images between registries
  - Handles multi-architecture images
  - Preserves image signatures during copy
  - Supports efficient layer transfer


* **Image signing and verification**:

  - Signs container images with GPG keys
  - Verifies image signatures
  - Manages signature policies
  - Ensures image authenticity


* **Image deletion**:

  - Removes images from registries
  - Handles cleanup operations
  - Respects registry policies


* **Credential management**:

  - Securely stores and retrieves registry credentials
  - Supports multiple authentication methods
  - Integrates with credential helpers

### Goals

* **Registry operations without runtime**: Enable users to work with container images without requiring a container runtime or daemon.

* **Secure image management**: Provide secure tools for inspecting, copying, and signing container images.

* **Efficient operations**: Optimize image operations for speed and efficiency, especially in CI/CD environments.

* **OCI compliance**: Maintain full compatibility with OCI specifications for container images.

* **Multi-registry support**: Work seamlessly with various container registries.

* **Image verification**: Provide robust tools for verifying image authenticity and integrity.

### Non-goals

* **Container runtime**: Skopeo does not run containers (that's handled by Podman, Docker, etc.).

* **Image building**: Skopeo does not build container images (that's handled by container engines such as Podman, Docker, Buildah, etc.).

* **Container orchestration**: Skopeo does not provide cluster orchestration capabilities.

* **Registry hosting**: Skopeo does not operate as a container registry, though it interacts with them.

## Self-assessment use

This self-assessment is created by the Skopeo team to perform an internal analysis of the project's security. It is not intended to provide a security audit of Skopeo, or function as an independent assessment or attestation of Skopeo's security health.

This document serves to provide Skopeo users with an initial understanding of Skopeo's security, where to find existing security documentation, Skopeo plans for security, and general overview of Skopeo security practices, both for development of Skopeo as well as security of Skopeo.

This document provides the CNCF TAG-Security with an initial understanding of Skopeo to assist in a joint-assessment, necessary for projects under incubation.  Taken together, this document and the joint-assessment serve as a cornerstone for if and when Skopeo seeks graduation and is preparing for a security audit.

## Security functions and features

### Critical Security Components

* **Signature verification**: Skopeo's core security feature that verifies container image signatures to ensure authenticity and integrity.

* **Secure registry communication**: All communication with registries uses secure protocols (HTTPS, TLS).

* **Credential protection**: Secure handling and storage of registry credentials.

* **Image inspection without download**: Ability to inspect images without downloading them, reducing exposure to potentially malicious content.

* **No daemon dependency**: Eliminates daemon-related security risks by operating as a standalone tool.

### Security Relevant Components

* **Sigstore integration**: Support for sigstore-based image signing and verification.

* **Policy enforcement**: Configurable policies for image acceptance and verification.

* **Multi-registry security**: Secure handling of credentials across multiple registries.

* **Audit logging**: Logging of security-relevant operations.

## Project compliance

* **OCI Compliance**: Skopeo is fully compliant with the Open Container Initiative (OCI) specifications for container images.

* **OpenSSF Best Practices**: Skopeo has achieved a [passing OpenSSF Best Practices badge](https://www.bestpractices.dev/projects/10516), demonstrating adherence to security best practices.

* **Image signing standards**: Supports standard image signing mechanisms including Sigstore.

## Secure development practices

### Development Pipeline

* **Code Review Process**: All code changes require review by at least one maintainer before merging. The project uses GitHub pull requests for all contributions.

* **Automated Testing**: Comprehensive test suite including unit tests, integration tests, and system tests that run on every pull request.

* **Security Scanning**: Automated vulnerability scanning of dependencies using tools like Dependabot and GitHub Security Advisories. All medium and higher severity exploitable vulnerabilities are fixed in a timely way after they are confirmed.

* **Static Analysis**: Code quality and security analysis using golangci-lint which is run on every PR, ensuring testing is done prior to merge. The tool includes rules to look for common vulnerabilities in Go code.

* **Dynamic Analysis**: Comprehensive test suite is run in CI on every PR and also on a nightly basis, testing the skopeo binary compiled using the PR's source code and the latest HEAD commit respectively.

* **OpenSSF Best Practices Compliance**: Skopeo has achieved a [passing OpenSSF Best Practices badge](https://www.bestpractices.dev/projects/10516), demonstrating adherence to security best practices including proper licensing, contribution guidelines, and security processes.

### Communication Channels

* Podman user room: [\#podman:fedoraproject.org](https://matrix.to/#/#podman:fedoraproject.org)

* Podman dev room: [\#podman-dev:matrix.org](https://matrix.to/#/#podman-dev:matrix.org)

* **Inbound**:

  - GitHub Issues for bug reports and feature requests
  - GitHub Discussions for community questions
  - Security issues via [security@lists.podman.io](mailto:security@lists.podman.io)
  - Mailing lists for formal discussions
  - Clear contribution guidelines documented in [CONTRIBUTING.md](https://github.com/containers/skopeo/blob/main/CONTRIBUTING.md)


* **Outbound**:

  - Release announcements via GitHub releases
  - Documentation updates and blog posts

### Ecosystem

Skopeo is a critical component of the cloud-native ecosystem:

* **CI/CD Pipelines**: Widely used in CI/CD systems for copying, inspecting, and verifying container images.

* **Registry Management**: Used by registry operators and administrators for managing container images across multiple registries.

* **Security Tools**: Integrated into security scanning and compliance tools for image verification.

## Security issue resolution

### Responsible Disclosures Process

* **Reporting**: Security vulnerabilities should be reported by email as documented in the [SECURITY.md](https://github.com/containers/skopeo/blob/main/SECURITY.md) file.

* **Response Time**: The team commits to responding to vulnerability reports within 48 hours. All medium and higher severity exploitable vulnerabilities are prioritized as a matter of general practice.

* **Credit**: Security researchers who responsibly disclose vulnerabilities are credited in security advisories and release notes.

* **Public Disclosure**: Vulnerabilities are disclosed by the project maintainers with appropriate embargo periods for critical issues, following industry best practices for responsible disclosure.

### Vulnerability Response Process

* **Triage**: Security reports are triaged by the security team and assigned severity levels (Critical, High, Medium, Low) using CVSS scoring where applicable.

* **Investigation**: The project maintainers investigate the vulnerability, determine impact, and develop fixes. All medium and higher severity exploitable vulnerabilities discovered are fixed in a timely way after they are confirmed.

* **Fix Development**: Security fixes are developed in private repositories to prevent premature disclosure. The project maintains a clear process for developing and testing security patches.

### Incident Response

* **Detection**: Security incidents in dependencies are detected and updated by tools like Renovate. Security issues are accepted through the Podman security mailing list.

* **Assessment**: The team assesses the severity and impact of security incidents using CVSS scoring and industry-standard severity classification.

* **Containment**: Immediate steps are taken to contain and mitigate the impact of security incidents. If the tests point out any issues in the development phase, those get fixed before any code is merged.


## Appendix

* **Security Advisories**: See [this NVD list](https://nvd.nist.gov/vuln/search#/nvd/home?vulnRevisionStatusList=published&keyword=skopeo&resultType=records) for CVEs to date.

* **Code Review**: The project's code review process has caught numerous potential security issues before they reach production.


### OpenSSF Best Practices

* **Current Status**: Skopeo has achieved a [passing OpenSSF Best Practices badge](https://www.bestpractices.dev/projects/10516) (100% compliance), demonstrating adherence to security best practices.

* **Key Achievements**:

  - Comprehensive project documentation and contribution guidelines
  - Robust security testing and analysis processes
  - Clear vulnerability disclosure and response procedures
  - Strong development practices with code review and automated testing
  - Proper licensing and project governance


### Related Projects / Vendors

* **Buildah**: A tool that facitiliates building OCI container images.

* **Podman:** A command line utility for managing OCI containers and pods.
