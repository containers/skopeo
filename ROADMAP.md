# Skopeo Roadmap

Skopeo intends to mostly continue to be a very thin CLI wrapper over the [https://github.com/containers/image](containers/image) library, with most features being added there, not to this repo. A typical new Skopeo feature would only add a CLI for a recent containers/image feature.

## Future feature focus (most of the work must be done in the containers/image library)

* OCI artifact support.
* Integration of composefs.
* Partial pull support (zstd:chunked).
* Performance and stability improvements.
* Reductions to the size of the Skopeo binary.
* `skopeo sync` exists, and bugs in it should be fixed, but we don’t have much of an ambition to compete with much larger projects like [https://github.com/openshift/oc-mirror](oc-mirror).
