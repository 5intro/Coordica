package runner

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"coordica/internal/domain"
)

func TestReviewSchemaPinsCandidateAndCannotAssertRunIdentity(t *testing.T) {
	candidate := strings.Repeat("a", 40)
	var schema struct {
		AdditionalProperties bool     `json:"additionalProperties"`
		Required             []string `json:"required"`
		Properties           map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(reviewOutputSchema(candidate, 1), &schema); err != nil {
		t.Fatal(err)
	}
	var fields []string
	typ := reflect.TypeOf(domain.Review{})
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name == "rules_digest" || name == "issues" || name == "resolutions" {
			continue
		}
		fields = append(fields, name)
	}
	slices.Sort(fields)
	slices.Sort(schema.Required)
	if schema.AdditionalProperties || !slices.Equal(fields, schema.Required) || len(schema.Properties) != len(fields) || !slices.Equal(schema.Properties["candidate"].Enum, []string{candidate}) || !slices.Equal(schema.Properties["run_id"].Enum, []string{""}) {
		t.Fatal("review schema drifted from exact domain contract or fixed identity", schema)
	}
}

func TestReviewSchemaLegacyAssignmentKeepsOriginalFields(t *testing.T) {
	var schema struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(reviewOutputSchema(strings.Repeat("b", 40), 0), &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 5 || schema.Properties["recovery"] != nil {
		t.Fatal("old center was sent a new review contract")
	}
}
