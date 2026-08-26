package openai

import (
	"context"
	"encoding/json"
	"testing"
)

// TestTransformRequest_CarriesJSONSchemaFromTextFormat pins the whole json_schema
// payload, not just its type.
//
// The converter used to copy `text.format.type` and nothing else, so every
// Structured Outputs request through /v1/responses lost its schema. The two
// downstream failure shapes are different and neither is loud:
//   - OpenAI-family upstreams reject `{"type":"json_schema"}` with no schema (400).
//   - The Gemini outbound (outbound/gemini/messages.go:358) only sets
//     ResponseSchema when JSONSchema != nil, otherwise it just asks for
//     application/json — the model returns JSON that does not follow the caller's
//     schema, with no error anywhere.
//
// Driven through TransformRequest rather than convertToInternalRequest so the
// json tags are exercised too: right struct fields with a wrong tag fails the
// same way, silently.
func TestTransformRequest_CarriesJSONSchemaFromTextFormat(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5",
		"input": "extract the city",
		"text": {
			"format": {
				"type": "json_schema",
				"name": "city_extraction",
				"description": "the city mentioned by the user",
				"strict": true,
				"schema": {"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}
			}
		}
	}`)

	req, err := (&ResponseInbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if req.ResponseFormat == nil {
		t.Fatal("ResponseFormat = nil, want the json_schema format")
	}
	if req.ResponseFormat.Type != "json_schema" {
		t.Fatalf("ResponseFormat.Type = %q, want %q", req.ResponseFormat.Type, "json_schema")
	}

	js := req.ResponseFormat.JSONSchema
	if js == nil {
		t.Fatal("ResponseFormat.JSONSchema = nil: the caller's schema was dropped, " +
			"which is a 400 on OpenAI upstreams and silently unconstrained output on Gemini")
	}
	if js.Name != "city_extraction" {
		t.Errorf("JSONSchema.Name = %q, want %q", js.Name, "city_extraction")
	}
	if js.Description != "the city mentioned by the user" {
		t.Errorf("JSONSchema.Description = %q, want the description from text.format", js.Description)
	}
	if js.Strict == nil {
		t.Error("JSONSchema.Strict = nil, want true: without strict the schema is a hint, not a constraint")
	} else if !*js.Strict {
		t.Errorf("JSONSchema.Strict = %v, want true", *js.Strict)
	}

	// Compare the schema semantically — asserting on raw bytes would break on any
	// whitespace change in the fixture above and tell us nothing about behaviour.
	var gotSchema, wantSchema any
	if err := json.Unmarshal(js.Schema, &gotSchema); err != nil {
		t.Fatalf("JSONSchema.Schema is not valid JSON (%q): %v", string(js.Schema), err)
	}
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`), &wantSchema); err != nil {
		t.Fatalf("fixture schema is not valid JSON: %v", err)
	}
	gotJSON, _ := json.Marshal(gotSchema)
	wantJSON, _ := json.Marshal(wantSchema)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("JSONSchema.Schema = %s, want %s", gotJSON, wantJSON)
	}
}

// TestTransformRequest_JSONObjectFormatLeavesSchemaNil guards the other half of
// the gate.
//
// model.ResponseFormatJSONSchema.Schema has no omitempty, so a JSONSchema built
// with an empty Schema marshals as `"schema":null` and upstreams reject that.
// Populating JSONSchema unconditionally would therefore trade "schema dropped"
// for "every json_object request 400s" — strictly worse. json_object legitimately
// carries no schema.
func TestTransformRequest_JSONObjectFormatLeavesSchemaNil(t *testing.T) {
	body := []byte(`{"model":"gpt-5","input":"hi","text":{"format":{"type":"json_object"}}}`)

	req, err := (&ResponseInbound{}).TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}

	if req.ResponseFormat == nil {
		t.Fatal("ResponseFormat = nil, want the json_object format")
	}
	if req.ResponseFormat.Type != "json_object" {
		t.Fatalf("ResponseFormat.Type = %q, want %q", req.ResponseFormat.Type, "json_object")
	}
	if req.ResponseFormat.JSONSchema != nil {
		t.Fatalf("ResponseFormat.JSONSchema = %#v, want nil: json_object has no schema, and an "+
			"empty one serialises to \"schema\":null, which upstreams reject", req.ResponseFormat.JSONSchema)
	}
}
