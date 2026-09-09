package transfer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Format is how a document is written.
type Format string

// The two formats.
const (
	FormatYAML Format = "yaml"
	FormatJSON Format = "json"
)

// ParseFormat reads a --format value.
func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case FormatYAML, "yml":
		return FormatYAML, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("format %q is not yaml or json", value)
	}
}

// DetectFormat works out what a file is by reading it, not by its name.
//
// A suffix is a claim somebody made about a file, and the two disagree often
// enough to matter: a .txt written by an export, a .yaml holding JSON because
// JSON is valid YAML and somebody knew that, a file with no suffix at all
// arriving over a pipe. The content is the fact.
//
// The test is the first character that is not whitespace or a comment. JSON
// documents here are objects, so a leading '{' settles it; YAML cannot begin
// that way unless it is a flow mapping, which nothing writes for a document
// with this shape.
//
// Note that JSON is a subset of YAML, so a JSON file parses either way and
// guessing wrong would still work. The detection exists to give a clear answer
// to "what is this", and to fail on a file that is neither with a sentence
// about the file rather than about a parser.
func DetectFormat(content []byte) (Format, error) {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "{") {
			return FormatJSON, nil
		}
		return FormatYAML, nil
	}
	return "", errors.New("the file is empty")
}

// Encode writes a document.
func Encode(doc *Document, format Format) ([]byte, error) {
	switch format {
	case FormatJSON:
		// Indented, because a person reads this: an export is something
		// somebody looks at, edits and sends to a colleague, and a single line
		// of JSON serves none of those.
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("writing JSON: %w", err)
		}
		return buf.Bytes(), nil

	case FormatYAML:
		var buf bytes.Buffer
		encoder := yaml.NewEncoder(&buf)
		encoder.SetIndent(2)
		if err := encoder.Encode(doc); err != nil {
			return nil, fmt.Errorf("writing YAML: %w", err)
		}
		if err := encoder.Close(); err != nil {
			return nil, fmt.Errorf("writing YAML: %w", err)
		}
		return buf.Bytes(), nil

	default:
		return nil, fmt.Errorf("format %q is not yaml or json", format)
	}
}

// Decode reads a document.
//
// Unknown fields are refused rather than ignored. A file with a mistyped key in
// it is a file somebody got wrong, and importing it silently with an empty name
// is worse than refusing it: the restaurant arrives, looks wrong, and nothing
// says why.
func Decode(content []byte, format Format) (*Document, error) {
	var doc Document

	switch format {
	case FormatJSON:
		decoder := json.NewDecoder(bytes.NewReader(content))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&doc); err != nil {
			return nil, fmt.Errorf("reading JSON: %w", err)
		}

	case FormatYAML:
		decoder := yaml.NewDecoder(bytes.NewReader(content))
		decoder.KnownFields(true)
		if err := decoder.Decode(&doc); err != nil {
			return nil, fmt.Errorf("reading YAML: %w", err)
		}

	default:
		return nil, fmt.Errorf("format %q is not yaml or json", format)
	}

	if doc.Version == 0 {
		return nil, errors.New("the file has no version field; it is not a doenerstag export")
	}
	if doc.Version > Version {
		return nil, fmt.Errorf(
			"the file is version %d and this doenerstag understands version %d; use a newer one",
			doc.Version, Version)
	}
	if len(doc.Restaurants) == 0 {
		return nil, errors.New("the file contains no restaurants")
	}
	return &doc, nil
}
