package ld

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeStringFromQuad(quad *Quad) string {
	if quad.Graph != nil {
		return fmt.Sprintf("%s %s %s %s", quad.Subject.GetValue(), quad.Predicate.GetValue(), quad.Object.GetValue(), quad.Graph.GetValue())
	}
	return fmt.Sprintf("%s %s %s", quad.Subject.GetValue(), quad.Predicate.GetValue(), quad.Object.GetValue())
}

func TestToRDFCallsRDFQuadProvenanceCallback(t *testing.T) {
	doc := `{
  "@context": {
    "name": "http://schema.org/name",
    "knows": {"@id": "http://schema.org/knows", "@type": "@id"}
  },
  "@id": "http://example.com/alice",
  "name": "Alice",
  "knows": {
    "name": {"@value": "Bob"},
    "@id": "http://example.com/bob"
  }
}`

	provenance := make(map[string]RDFQuadProvenance)
	opts := NewJsonLdOptions("")
	opts.RDFQuadProvenanceCallback = func(quad *Quad, prov RDFQuadProvenance) {
		provenance[makeStringFromQuad(quad)] = prov
	}

	proc := NewJsonLdProcessor()
	reader := strings.NewReader(doc)
	result, err := proc.ToRDF(reader, opts)
	require.NoError(t, err)

	dataset := result.(*RDFDataset)
	require.Len(t, dataset.Graphs["@default"], 3)
	assert.Equal(t, RDFQuadProvenance{
		SubjectLine:   1,
		PredicateLine: 7,
		ObjectLine:    7,
	}, provenance["http://example.com/alice http://schema.org/name Alice"])
	assert.Equal(t, RDFQuadProvenance{
		SubjectLine:   1,
		PredicateLine: 8,
		ObjectLine:    8,
	}, provenance["http://example.com/alice http://schema.org/knows http://example.com/bob"])
	assert.Equal(t, RDFQuadProvenance{
		SubjectLine:   8,
		PredicateLine: 9,
		ObjectLine:    9,
	}, provenance["http://example.com/bob http://schema.org/name Bob"])
}

func TestToRDFWithNoCallback(t *testing.T) {
	doc := `{
  "@context": {
    "name": "http://schema.org/name",
    "knows": {"@id": "http://schema.org/knows", "@type": "@id"}
  },
  "@id": "http://example.com/alice",
  "name": "Alice",
  "knows": {
    "name": {"@value": "Bob"},
    "@id": "http://example.com/bob"
  }
}`

	opts := NewJsonLdOptions("")
	opts.RDFQuadProvenanceCallback = nil

	proc := NewJsonLdProcessor()
	reader := strings.NewReader(doc)
	_, err := proc.ToRDF(reader, opts)
	require.NoError(t, err)
}
