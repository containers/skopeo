// Copyright 2016 The Linux Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package schema

import (
	"embed"
	"net/http"

	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// Media types for the OCI image formats
const (
	ValidatorMediaTypeDescriptor   Validator     = v1.MediaTypeDescriptor
	ValidatorMediaTypeLayoutHeader Validator     = v1.MediaTypeLayoutHeader
	ValidatorMediaTypeManifest     Validator     = v1.MediaTypeImageManifest
	ValidatorMediaTypeImageIndex   Validator     = v1.MediaTypeImageIndex
	ValidatorMediaTypeImageConfig  Validator     = v1.MediaTypeImageConfig
	ValidatorMediaTypeImageLayer   unimplemented = v1.MediaTypeImageLayer
)

var (
	// fs stores the embedded http.FileSystem
	// having the OCI JSON schema files in root "/".
	//go:embed *.json
	fs embed.FS

	// schemaNamespaces is a set of URI prefixes which are treated as containing the schema files of fs.
	// This is necessary because *.json schema files in this directory use "id" and "$ref" attributes which evaluate to such URIs, e.g.
	// ./image-manifest-schema.json URI contains
	//   "id": "https://opencontainers.org/schema/image/manifest",
	// and
	//   "$ref": "content-descriptor.json"
	// which evaluates as a link to https://opencontainers.org/schema/image/content-descriptor.json .
	//
	// To support such links without accessing the network (and trying to load content which is not hosted at these URIs),
	// fsLoaderFactory accepts any URI starting with one of the schemaNamespaces below,
	// and uses _escFS to load them from the root of its in-memory filesystem tree.
	//
	// (Note that this must contain subdirectories before its parent directories for fsLoaderFactory.refContents to work.)
	schemaNamespaces = []string{
		"https://opencontainers.org/schema/image/descriptor/",
		"https://opencontainers.org/schema/image/index/",
		"https://opencontainers.org/schema/image/manifest/",
		"https://opencontainers.org/schema/image/",
		"https://opencontainers.org/schema/descriptor/",
		"https://opencontainers.org/schema/",
	}

	// specs maps OCI schema media types to schema URIs.
	// These URIs are expected to be used only by fsLoaderFactory (which trims schemaNamespaces defined above)
	// and should never cause a network access.
	specs = map[Validator]string{
		ValidatorMediaTypeDescriptor:   "https://opencontainers.org/schema/content-descriptor.json",
		ValidatorMediaTypeLayoutHeader: "https://opencontainers.org/schema/image/image-layout-schema.json",
		ValidatorMediaTypeManifest:     "https://opencontainers.org/schema/image/image-manifest-schema.json",
		ValidatorMediaTypeImageIndex:   "https://opencontainers.org/schema/image/image-index-schema.json",
		ValidatorMediaTypeImageConfig:  "https://opencontainers.org/schema/image/config-schema.json",
	}
)

// FileSystem returns an in-memory filesystem including the schema files.
// The schema files are located at the root directory.
func FileSystem() http.FileSystem {
	return http.FS(fs)
}
