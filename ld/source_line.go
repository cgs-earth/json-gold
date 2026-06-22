// Copyright 2015-2017 Piprate Limited
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

package ld

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"sort"
)

type sourceLineMetadata struct {
	line       int
	properties map[string]int
}

// sourceLineStore tracks source line numbers for JSON-LD objects and
// properties. It maps metadata by each map's runtime identity (uintptr) because
// map[string]any is not comparable and cannot be used directly as a map key,
// Also because hashing contents would be unstable as JSON-LD processing mutates
// maps. Keeping this as a sidecar also avoids adding extra magic fields to an
// existing JSON-LD map.
type sourceLineStore struct {
	objects map[uintptr]*sourceLineMetadata
}

// newSourceLineStore creates the sidecar store used to keep source line data out
// of decoded JSON-LD maps while they move through expansion and RDF conversion.
func newSourceLineStore() *sourceLineStore {
	return &sourceLineStore{
		objects: make(map[uintptr]*sourceLineMetadata),
	}
}

// documentFromReaderWithSourceLines parses JSON-LD input and records object and
// property line numbers for later RDFQuadProvenance callbacks.
func documentFromReaderWithSourceLines(r io.Reader) (any, *sourceLineStore, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, NewJsonLdError(LoadingDocumentFailed, err)
	}

	newlineOffsets := make([]int64, 0)
	for i, b := range data {
		if b == '\n' {
			newlineOffsets = append(newlineOffsets, int64(i))
		}
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	store := newSourceLineStore()
	document, err := decodeValueWithSourceLines(dec, newlineOffsets, store)
	if err != nil {
		return nil, nil, NewJsonLdError(LoadingDocumentFailed, err)
	}
	return document, store, nil
}

// decodeValueWithSourceLines mirrors json.Decoder's normal recursive decoding
// while attaching line metadata to each object and object property encountered.
func decodeValueWithSourceLines(dec *json.Decoder, newlineOffsets []int64, store *sourceLineStore) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}

	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			line := lineForOffset(dec.InputOffset()-1, newlineOffsets)
			m := make(map[string]any)
			store.SetLine(m, line)
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := keyTok.(string)
				propertyLine := lineForOffset(dec.InputOffset(), newlineOffsets)
				val, err := decodeValueWithSourceLines(dec, newlineOffsets, store)
				if err != nil {
					return nil, err
				}
				m[key] = val
				store.SetPropertyLine(m, key, propertyLine)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return m, nil
		case '[':
			a := make([]any, 0)
			for dec.More() {
				val, err := decodeValueWithSourceLines(dec, newlineOffsets, store)
				if err != nil {
					return nil, err
				}
				a = append(a, val)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return a, nil
		}
	}

	return tok, nil
}

// lineForOffset converts a decoder byte offset into a 1-based source line using
// the precomputed newline positions from the original input.
func lineForOffset(offset int64, newlineOffsets []int64) int {
	return sort.Search(len(newlineOffsets), func(i int) bool {
		return newlineOffsets[i] >= offset
	}) + 1
}

// Line returns the source line for a JSON-LD object map, or 0 when the line is
// unknown or provenance tracking is disabled.
func (s *sourceLineStore) Line(v any) int {
	if s == nil {
		return 0
	}
	if m, ok := v.(map[string]any); ok {
		if metadata := s.get(m); metadata != nil {
			return metadata.line
		}
	}
	return 0
}

// LineWithFallback keeps generated RDF provenance populated when a transformed
// value has no direct object line but its containing construct does.
func (s *sourceLineStore) LineWithFallback(v any, fallback int) int {
	if line := s.Line(v); line > 0 {
		return line
	}
	return fallback
}

// SetLine records the line where a JSON-LD object begins so later processing can
// attribute RDF subjects or object values back to the original document.
func (s *sourceLineStore) SetLine(m map[string]any, line int) {
	if s == nil {
		return
	}
	if line > 0 {
		metadata := s.ensure(m)
		metadata.line = line
	}
}

// SetLineIfMissing preserves the earliest known source location for maps copied
// or synthesized during expansion without overwriting more precise metadata.
func (s *sourceLineStore) SetLineIfMissing(m map[string]any, line int) {
	if s == nil {
		return
	}
	if line > 0 && s.Line(m) == 0 {
		s.SetLine(m, line)
	}
}

// SetLineOnValue applies a property line to expanded values that do not have
// their own object line, including each object nested inside arrays.
func (s *sourceLineStore) SetLineOnValue(v any, line int) {
	if s == nil {
		return
	}
	switch val := v.(type) {
	case map[string]any:
		s.SetLineIfMissing(val, line)
	case []any:
		for _, item := range val {
			s.SetLineOnValue(item, line)
		}
	}
}

// PropertyLine returns the line where a property key appeared on a JSON-LD
// object, which becomes predicate provenance during RDF generation.
func (s *sourceLineStore) PropertyLine(v any, property string) int {
	if s == nil {
		return 0
	}
	if m, ok := v.(map[string]any); ok {
		if metadata := s.get(m); metadata != nil {
			return metadata.properties[property]
		}
	}
	return 0
}

// SetPropertyLine records a property's source line on an object map so expanded
// properties can be traced back to their original JSON-LD keys.
func (s *sourceLineStore) SetPropertyLine(m map[string]any, property string, line int) {
	if s == nil {
		return
	}
	if line > 0 {
		metadata := s.ensure(m)
		if metadata.properties == nil {
			metadata.properties = make(map[string]int)
		}
		metadata.properties[property] = line
	}
}

// sourceLineProvenance packages the tracked line numbers into the callback
// payload emitted with generated RDF quads.
func sourceLineProvenance(subjectLine int, predicateLine int, objectLine int, graphLine int) RDFQuadProvenance {
	return RDFQuadProvenance{
		SubjectLine:   subjectLine,
		PredicateLine: predicateLine,
		ObjectLine:    objectLine,
		GraphLine:     graphLine,
	}
}

// applyProvCallback invokes the optional provenance callback only for valid RDF
// quads, matching the point where tracked lines become observable to callers.
func applyProvCallback(callback RDFQuadProvenanceCallback, quad *Quad, provenance RDFQuadProvenance) {
	if callback != nil && quad != nil && quad.Valid() {
		callback(quad, provenance)
	}
}

// get looks up metadata by map identity so source lines survive without adding
// synthetic fields to user JSON-LD objects.
func (s *sourceLineStore) get(m map[string]any) *sourceLineMetadata {
	if s == nil {
		return nil
	}
	return s.objects[mapIdentity(m)]
}

// ensure returns the metadata bucket for a map, creating it when a parser or
// transformation step first discovers line information for that object.
func (s *sourceLineStore) ensure(m map[string]any) *sourceLineMetadata {
	if metadata := s.get(m); metadata != nil {
		return metadata
	}
	metadata := &sourceLineMetadata{
		properties: make(map[string]int),
	}
	s.objects[mapIdentity(m)] = metadata
	return metadata
}

// mapIdentity returns the map header pointer as a uintptr so metadata is mapped
// by object identity, not object contents. That keeps distinct but equal maps
// separate and avoids recomputing a content hash after expansion mutates them.
func mapIdentity(m map[string]any) uintptr {
	return reflect.ValueOf(m).Pointer()
}
