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
	if err := json.Unmarshal(reviewOutputSchema(candidate), &schema); err != nil {
		t.Fatal(err)
	}
	var fields []string
	typ := reflect.TypeOf(domain.Review{})
	for i := 0; i < typ.NumField(); i++ {
		fields = append(fields, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
	}
	slices.Sort(fields)
	slices.Sort(schema.Required)
	if schema.AdditionalProperties || !slices.Equal(fields, schema.Required) || len(schema.Properties) != len(fields) || !slices.Equal(schema.Properties["candidate"].Enum, []string{candidate}) || !slices.Equal(schema.Properties["run_id"].Enum, []string{""}) {
		t.Fatal("review schema drifted from exact domain contract or fixed identity", schema)
	}
}
