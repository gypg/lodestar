package openai

import (
	"encoding/json"
	"testing"

	"github.com/gypg/lodestar/internal/transformer/model"
)

// TestConvertToResponsesRequest_FlattensJSONSchema pins the outbound half of the
// Structured Outputs path.
//
// The internal model keeps json_schema nested (ResponseFormat.JSONSchema — the
// chat completions shape); the Responses API wants name/schema/strict/description
// flattened into text.format. The converter used to emit only `type`, so the
// schema was lost regardless of which endpoint the client came in on: a
// /v1/chat/completions caller routed to a Responses-format upstream lost it here
// even though the inbound side had parsed it correctly.
//
// Asserted through the marshalled wire body, not the struct: correct fields under
// a wrong json tag never reach the upstream and would pass a struct-only check.
func TestConvertToResponsesRequest_FlattensJSONSchema(t *testing.T) {
	strict := true
	req := &model.InternalLLMRequest{
		Model: "gpt-5",
		ResponseFormat: &model.ResponseFormat{
			Type: "json_schema",
			JSONSchema: &model.ResponseFormatJSONSchema{
				Name:        "city_extraction",
				Description: "the city mentioned by the user",
				Schema:      json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
				Strict:      &strict,
			},
		},
	}

	body, err := json.Marshal(ConvertToResponsesRequest(req))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var wire struct {
		Text *struct {
			Format *struct {
				Type        string          `json:"type"`
				Name        string          `json:"name"`
				Description string          `json:"description"`
				Schema      json.RawMessage `json:"schema"`
				Strict      *bool           `json:"strict"`
			} `json:"format"`
		} `json:"text"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
	}
	if wire.Text == nil || wire.Text.Format == nil {
		t.Fatalf("wire body has no text.format: %s", body)
	}
	f := wire.Text.Format

	if f.Type != "json_schema" {
		t.Errorf("text.format.type = %q, want %q", f.Type, "json_schema")
	}
	if f.Name != "city_extraction" {
		t.Errorf("text.format.name = %q, want %q", f.Name, "city_extraction")
	}
	if f.Description != "the city mentioned by the user" {
		t.Errorf("text.format.description = %q, want the description from the internal request", f.Description)
	}
	if f.Strict == nil {
		t.Error("text.format.strict missing, want true: without it the upstream treats the schema as advisory")
	} else if !*f.Strict {
		t.Errorf("text.format.strict = %v, want true", *f.Strict)
	}

	var gotSchema, wantSchema any
	if err := json.Unmarshal(f.Schema, &gotSchema); err != nil {
		t.Fatalf("text.format.schema is not valid JSON (%q): %v", string(f.Schema), err)
	}
	if err := json.Unmarshal([]byte(`{"type":"object","properties":{"city":{"type":"string"}}}`), &wantSchema); err != nil {
		t.Fatalf("fixture schema is not valid JSON: %v", err)
	}
	gotJSON, _ := json.Marshal(gotSchema)
	wantJSON, _ := json.Marshal(wantSchema)
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("text.format.schema = %s, want %s", gotJSON, wantJSON)
	}
}

// TestConvertToResponsesRequest_JSONObjectSendsTypeOnly checks the converter does
// not invent schema keys when the internal request has no nested schema.
//
// json_object is a valid format with no schema. Emitting `"schema":null` or an
// empty name alongside it turns a working request into an upstream 400, so the
// flattening must stay conditional on JSONSchema being present.
func TestConvertToResponsesRequest_JSONObjectSendsTypeOnly(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:          "gpt-5",
		ResponseFormat: &model.ResponseFormat{Type: "json_object"},
	}

	body, err := json.Marshal(ConvertToResponsesRequest(req))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var wire struct {
		Text *struct {
			Format map[string]any `json:"format"`
		} `json:"text"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
	}
	if wire.Text == nil || wire.Text.Format == nil {
		t.Fatalf("wire body has no text.format: %s", body)
	}

	if got := wire.Text.Format["type"]; got != "json_object" {
		t.Errorf("text.format.type = %v, want %q", got, "json_object")
	}
	for _, key := range []string{"schema", "name", "strict", "description"} {
		if _, present := wire.Text.Format[key]; present {
			t.Errorf("text.format.%s present for json_object (%s), want it omitted", key, body)
		}
	}
}
