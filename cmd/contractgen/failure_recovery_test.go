package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestEmbeddedFailureRecoverySchemaIsStrictAndNonNullable(t *testing.T) {
	raw := failureRecoverySchema()
	embedded, err := os.ReadFile("../coordica/failure-recovery.schema.json")
	if err != nil || !bytes.Equal(embedded, raw) {
		t.Fatal("regenerate embedded failure contract", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatal("unknown actions could enter native contract")
	}
	props := schema["properties"].(map[string]any)
	if len(props) != 4 || props["tasks"].(map[string]any)["type"] != "array" {
		t.Fatal("missing finite nonnull task proposals", props)
	}
}
