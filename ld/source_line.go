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
	"sort"
)

// sourceLineMetadataKey is a magic key used to store source line metadata
// in JSON-LD.
const sourceLineMetadataKey = "\x00json-gold:source-line"

type sourceLineMetadata struct {
	line       int
	properties map[string]int
}

func documentFromReaderWithSourceLines(r io.Reader) (any, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, NewJsonLdError(LoadingDocumentFailed, err)
	}

	newlineOffsets := make([]int64, 0)
	for i, b := range data {
		if b == '\n' {
			newlineOffsets = append(newlineOffsets, int64(i))
		}
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	document, err := decodeValueWithSourceLines(dec, newlineOffsets)
	if err != nil {
		return nil, NewJsonLdError(LoadingDocumentFailed, err)
	}
	return document, nil
}

func decodeValueWithSourceLines(dec *json.Decoder, newlineOffsets []int64) (any, error) {
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
			metadata := &sourceLineMetadata{
				line:       line,
				properties: make(map[string]int),
			}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key := keyTok.(string)
				propertyLine := lineForOffset(dec.InputOffset(), newlineOffsets)
				val, err := decodeValueWithSourceLines(dec, newlineOffsets)
				if err != nil {
					return nil, err
				}
				m[key] = val
				metadata.properties[key] = propertyLine
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			m[sourceLineMetadataKey] = metadata
			return m, nil
		case '[':
			a := make([]any, 0)
			for dec.More() {
				val, err := decodeValueWithSourceLines(dec, newlineOffsets)
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

func lineForOffset(offset int64, newlineOffsets []int64) int {
	return sort.Search(len(newlineOffsets), func(i int) bool {
		return newlineOffsets[i] >= offset
	}) + 1
}

func sourceLine(v any) int {
	if m, ok := v.(map[string]any); ok {
		if metadata := getSourceLineMetadata(m); metadata != nil {
			return metadata.line
		}
	}
	return 0
}

func sourceLineWithFallback(v any, fallback int) int {
	if line := sourceLine(v); line > 0 {
		return line
	}
	return fallback
}

func setSourceLine(m map[string]any, line int) {
	if line > 0 {
		metadata := ensureSourceLineMetadata(m)
		metadata.line = line
	}
}

func setSourceLineIfMissing(m map[string]any, line int) {
	if line > 0 && sourceLine(m) == 0 {
		setSourceLine(m, line)
	}
}

func setSourceLineOnValue(v any, line int) {
	switch val := v.(type) {
	case map[string]any:
		setSourceLineIfMissing(val, line)
	case []any:
		for _, item := range val {
			setSourceLineOnValue(item, line)
		}
	}
}

func sourcePropertyLine(v any, property string) int {
	if m, ok := v.(map[string]any); ok {
		if metadata := getSourceLineMetadata(m); metadata != nil {
			return metadata.properties[property]
		}
	}
	return 0
}

func setSourcePropertyLine(m map[string]any, property string, line int) {
	if line > 0 {
		metadata := ensureSourceLineMetadata(m)
		if metadata.properties == nil {
			metadata.properties = make(map[string]int)
		}
		metadata.properties[property] = line
	}
}

// Return the length of the map without the special sourceLineMetadataKey
// which tracks the source line of nodes in the JSON-LD document
func lenWithoutProvMetadata(m map[string]any) int {
	if _, ok := m[sourceLineMetadataKey]; ok {
		return len(m) - 1
	}
	return len(m)
}

// Check if the key is the magic sourceLineMetadataKey value
func isSourceLineMetadataKey(key string) bool {
	return key == sourceLineMetadataKey
}

func sourceLineProvenance(subjectLine int, predicateLine int, objectLine int, graphLine int) RDFQuadProvenance {
	return RDFQuadProvenance{
		SubjectLine:   subjectLine,
		PredicateLine: predicateLine,
		ObjectLine:    objectLine,
		GraphLine:     graphLine,
	}
}

func applyProvCallback(callback RDFQuadProvenanceCallback, quad *Quad, provenance RDFQuadProvenance) {
	if callback != nil && quad != nil && quad.Valid() {
		callback(quad, provenance)
	}
}

func getSourceLineMetadata(m map[string]any) *sourceLineMetadata {
	metadata, _ := m[sourceLineMetadataKey].(*sourceLineMetadata)
	return metadata
}

func ensureSourceLineMetadata(m map[string]any) *sourceLineMetadata {
	if metadata := getSourceLineMetadata(m); metadata != nil {
		return metadata
	}
	metadata := &sourceLineMetadata{
		properties: make(map[string]int),
	}
	m[sourceLineMetadataKey] = metadata
	return metadata
}
