% skopeo-experimental-image-proxy(1)

# NAME
skopeo-experimental-image-proxy - API server for fetching container images (EXPERIMENTAL)

# SYNOPSIS
**skopeo experimental-image-proxy** [*options*]

# DESCRIPTION
**EXPERIMENTAL COMMAND**: This command is experimental, and its API is subject to change. It is currently hidden from the main help output and not supported on Windows.

`skopeo experimental-image-proxy` exposes core container image fetching APIs via custom JSON+fd-passing protocol. This provides a lightweight way to fetch container image content (manifests and blobs). This command is primarily intended for programs that want to operate on a storage type that skopeo doesn't natively handle. For example, the bootc project currently has a custom ostree-based container storage backend.

The client process that invokes `skopeo experimental-image-proxy` is responsible for creating a socket pair and passing one of the file descriptors to the proxy. By default, the proxy expects this file descriptor to be its standard input (fd 0), but a different fd can be specified using the **--sockfd** option.

**Protocol Overview**

The protocol requires a `socketpair(2)` of type `SOCK_SEQPACKET`, over which a single JSON message is sent per packet. Large data payloads, such as image manifests and blobs, are transferred over separate pipes (`pipe(2)`), with the read-ends of these pipes passed to the client via file descriptor (FD) passing.

*   **Request Format**: A JSON object: `{ "method": "MethodName", "args": [arguments] }`
*   **Reply Format**: A JSON object: `{ "success": boolean, "value": JSONValue, "pipeid": number, "error_code": string, "error": string }`
    *   `success`: `true` if the call succeeded, `false` otherwise.
    *   `value`: The return value of the method, if any.
    *   `pipeid`: An integer identifying a pipe for data transfer. This ID is used with the `FinishPipe` method.
    *   `error_code`: A string indicating the type of error if `success` is `false` (e.g., "EPIPE", "retryable", "other"). (Introduced in protocol version 0.2.8)
    *   `error`: A string describing the error if `success` is `false`.

The current protocol version is `0.2.8`.

**Supported Protocol Methods**

The server supports the following methods:

*   **Initialize**: Initializes the proxy. This method must be called before any other method.
    *   Args: `[]` (empty array)
    *   Returns: `string` (the protocol version, e.g., "0.2.8")
*   **OpenImage**: Opens an image reference (e.g., `docker://quay.io/example/image:latest`).
    *   Args: `[string imageName]`
    *   Returns: `uint64` (an opaque image ID to be used in subsequent calls)
*   **OpenImageOptional**: Similar to `OpenImage`, but if the image is not found, it returns `0` (a sentinel image ID) instead of an error.
    *   Args: `[string imageName]`
    *   Returns: `uint64` (opaque image ID, or `0` if the image is not found)
*   **CloseImage**: Closes a previously opened image, releasing associated resources.
    *   Args: `[uint64 imageID]`
    *   Returns: `null`
*   **GetManifest**: Retrieves the image manifest. If the image is a manifest list, it is resolved to an image matching the proxy's current OS and architecture. The manifest is converted to OCI format if it isn't already. The `value` field in the reply contains the original digest of the manifest (if the image is a manifest list, this is the digest of the list, not the per-platform instance). The manifest content is streamed over a pipe.
    *   Args: `[uint64 imageID]`
    *   Returns: `string` (manifest digest in `value`), manifest data via pipe.
*   **GetFullConfig**: Retrieves the full image configuration, conforming to the OCI Image Format Specification. Configuration data is streamed over a pipe.
    *   Args: `[uint64 imageID]`
    *   Returns: `null`, configuration data via pipe.
*   **GetBlob**: Fetches an image blob (e.g., a layer) by its digest and expected size. The proxy performs digest verification on the blob data. The `value` field in the reply contains the blob size. Blob data is streamed over a pipe.
    *   Args: `[uint64 imageID, string digest, uint64 size]`
    *   Returns: `int64` (blob size in `value`, `-1` if unknown), blob data via pipe.
*   **GetRawBlob**: Fetches an image blob by its digest. Unlike `GetBlob`, this method does not perform server-side digest verification. It returns two file descriptors to the client: one for the blob data and another for reporting errors that occur during the streaming. This method does not use the `FinishPipe` mechanism. The `value` field in the reply contains the blob size. (Introduced in protocol version 0.2.8)
    *   Args: `[uint64 imageID, string digest]`
    *   Returns: `int64` (blob size in `value`, `-1` if unknown), and *two* file descriptors: one for the blob data, one for errors. The error is a `ProxyError` type, see below.
*   **GetLayerInfoPiped**: Retrieves information about image layers. This replaces `GetLayerInfo`.  Layer information data is streamed over a pipe, which makes it more reliable for images with many layers that would exceed message size limits with `GetLayerInfo`. The returned data is a JSON array of `{digest: string, size: int64, media_type: string}`. (Introduced in protocol version 0.2.7)
    *   Args: `[uint64 imageID]`
    *   Returns: `null`, layer information data via pipe.
*   **FinishPipe**: Signals that the client has finished reading all data from a pipe associated with a `pipeid` (obtained from methods like `GetManifest` or `GetBlob`). This allows the server to close its end of the pipe and report any pending errors (e.g., digest verification failure for `GetBlob`). This method **must** be called by the client after consuming data from a pipe, except for pipes from `GetRawBlob`.
    *   Args: `[uint32 pipeID]`
    *   Returns: `null`
*   **Shutdown**: Instructs the proxy server to terminate gracefully.
    *   Args: `[]` (empty array)
    *   Returns: `null`

The following methods are deprecated:

*   **GetConfig**: (deprecated) Retrieves the container runtime configuration part of the image (the OCI `config` field). **Note**: This method returns only a part of the full image configuration due to a historical oversight. Use `GetFullConfig` for the complete image configuration. Configuration data is streamed over a pipe.
    *   Args: `[uint64 imageID]`
    *   Returns: `null`, configuration data via pipe.
*   **GetLayerInfo**: (deprecated) Retrieves an array of objects, each describing an image layer (digest, size, mediaType). **Note**: This method returns data inline and may fail for images with many layers due to message size limits. Use `GetLayerInfoPiped` for a more robust solution.
    *   Args: `[uint64 imageID]`
    *   Returns: `array` of `{digest: string, size: int64, media_type: string}`.

**Data Transfer for Pipes**

When a method returns a `pipeid`, the server also passes the read-end of a pipe via file descriptor (FD) passing. The client reads the data (e.g., manifest content, blob content) from this FD. After successfully reading all data, the client **must** call `FinishPipe` with the corresponding `pipeid`. This signals to the server that the transfer is complete, allows the server to clean up resources, and enables the client to check for any errors that might have occurred during the data streaming process (e.g., a digest mismatch during `GetBlob`). The `GetRawBlob` method is an exception; it uses a dedicated error pipe instead of the `FinishPipe` mechanism.

**ProxyError**

`GetBlobRaw` returns a JSON object of the following form in the error pipe where:
```
{
    "code": "EPIPE" | "retryable" | "other",
    "message": "error message"
}
```

- EPIPE: The client closed the pipe before reading all data.
- retryable: The operation failed but might succeed if retried.
- other: A generic error occurred.

# OPTIONS
**--sockfd**=*fd*
Serve on the opened socket passed as file descriptor *fd*. Defaults to 0 (standard input).

The command also supports common skopeo options for interacting with image registries and local storage. These include:

**--authfile**=*path*

Path of the primary registry credentials file. On Linux, the default is ${XDG\_RUNTIME\_DIR}/containers/auth.json.
See **containers-auth.json**(5) for more details about the credential search mechanism and defaults on other platforms.

Use `skopeo login` to manage the credentials.

The default value of this option is read from the `REGISTRY\_AUTH\_FILE` environment variable.

**--cert-dir**=*path*

Use certificates at *path* (\*.crt, \*.cert, \*.key) to connect to the registry.

**--creds** _username[:password]_

Username and password for accessing the registry.

**--daemon-host** _host_

Use docker daemon host at _host_ (`docker-daemon:` transport only)

**--no-creds**

Access the registry anonymously.

**--password**=*password*

Password for accessing the registry. Use with **--username**.

**--registry-token**=*token*

Provide a Bearer *token* for accessing the registry.

**--shared-blob-dir** _directory_

Directory to use to share blobs across OCI repositories.

**--tls-verify**=_bool_

Require HTTPS and verify certificates when talking to the container registry or daemon. Default to registry.conf setting.

**--username**=*username*
Username for accessing the registry. Use with **--password**.

# REFERENCE CLIENT LIBRARIES

- Rust: The [containers-image-proxy-rs project](https://github.com/containers/containers-image-proxy-rs) serves
as the reference Rust client.

# PROTOCOL HISTORY

- 0.2.1: Initial version
- 0.2.2: Added support for fetching image configuration as OCI
- 0.2.3: Added GetFullConfig
- 0.2.4: Added OpenImageOptional
- 0.2.5: Added LayerInfoJSON
- 0.2.6: Policy Verification before pulling OCI
- 0.2.7: Added GetLayerInfoPiped
- 0.2.8: Added GetRawBlob and error_code to replies

## SEE ALSO
skopeo(1), containers-auth.json(5)
