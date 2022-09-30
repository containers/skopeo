// Copyright 2018 The Linux Foundation
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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/xeipuuv/gojsonreference"
	"github.com/xeipuuv/gojsonschema"
)

// fsLoaderFactory implements gojsonschema.JSONLoaderFactory by reading files under the specified namespaces from the root of fs.
type fsLoaderFactory struct {
	namespaces []string
	fs         http.FileSystem
}

// newFSLoaderFactory returns a fsLoaderFactory reading files under the specified namespaces from the root of fs.
func newFSLoaderFactory(namespaces []string, fs http.FileSystem) *fsLoaderFactory {
	return &fsLoaderFactory{
		namespaces: namespaces,
		fs:         fs,
	}
}

func (factory *fsLoaderFactory) New(source string) gojsonschema.JSONLoader {
	return &fsLoader{
		factory: factory,
		source:  source,
	}
}

// refContents returns the contents of ref, if available in fsLoaderFactory.
func (factory *fsLoaderFactory) refContents(ref gojsonreference.JsonReference) ([]byte, error) {
	refStr := ref.String()
	path := ""
	for _, ns := range factory.namespaces {
		if strings.HasPrefix(refStr, ns) {
			path = "/" + strings.TrimPrefix(refStr, ns)
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("schema reference %#v unexpectedly not available in fsLoaderFactory with namespaces %#v", path, factory.namespaces)
	}

	f, err := factory.fs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

// fsLoader implements gojsonschema.JSONLoader by reading the document named by source from a fsLoaderFactory.
type fsLoader struct {
	factory *fsLoaderFactory
	source  string
}

// JsonSource implements gojsonschema.JSONLoader.JsonSource. The "Json" capitalization needs to be maintained to conform to the interface.
func (l *fsLoader) JsonSource() interface{} { // revive:disable-line:var-naming
	return l.source
}

func (l *fsLoader) LoadJSON() (interface{}, error) {
	// Based on gojsonschema.jsonReferenceLoader.LoadJSON.
	reference, err := gojsonreference.NewJsonReference(l.source)
	if err != nil {
		return nil, err
	}

	refToURL := reference
	refToURL.GetUrl().Fragment = ""

	body, err := l.factory.refContents(refToURL)
	if err != nil {
		return nil, err
	}

	return decodeJSONUsingNumber(bytes.NewReader(body))
}

// decodeJSONUsingNumber returns JSON parsed from an io.Reader
func decodeJSONUsingNumber(r io.Reader) (interface{}, error) {
	// Copied from gojsonschema.
	var document interface{}

	decoder := json.NewDecoder(r)
	decoder.UseNumber()

	err := decoder.Decode(&document)
	if err != nil {
		return nil, err
	}

	return document, nil
}

// JsonReference implements gojsonschema.JSONLoader.JsonReference. The "Json" capitalization needs to be maintained to conform to the interface.
func (l *fsLoader) JsonReference() (gojsonreference.JsonReference, error) { // revive:disable-line:var-naming
	return gojsonreference.NewJsonReference(l.JsonSource().(string))
}

func (l *fsLoader) LoaderFactory() gojsonschema.JSONLoaderFactory {
	return l.factory
}
