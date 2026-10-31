package jsoninput

import "testing"

func TestNullableReportsKeepStrictCommandAndEnvelopeValidation(t *testing.T) {
	type report struct {
		Result *bool `json:"result"`
	}
	raw := []byte(`{"result":null}`)
	var value report
	if err := Decode(raw, &value, 1024); err == nil {
		t.Fatal("command decoder accepted null")
	}
	if err := DecodeNullable(raw, &value, 1024); err != nil || value.Result != nil {
		t.Fatal("external nullable report was not preserved", value, err)
	}
	if err := Required(raw, "result"); err == nil {
		t.Fatal("null satisfied a required result")
	}
	for _, raw := range []string{
		`{"result":null,"result":false}`, `{"unknown":null}`, `{"result":false} {}`,
		`{"result":1}`, `null`, `[]`, `{"result":null`,
	} {
		if err := DecodeNullable([]byte(raw), &value, 1024); err == nil {
			t.Fatal("nullable decoder weakened envelope validation", raw)
		}
	}
	if err := DecodeNullable(raw, &value, 1); err == nil {
		t.Fatal("nullable report exceeded size bound")
	}
	if err := DecodeNullable([]byte(`{"result":false}`), &value, 1024); err != nil || value.Result == nil || *value.Result {
		t.Fatal("explicit false was not preserved", value, err)
	}
}
