package domain

import "encoding/json"

// Legacy reviews omit new fields. V1 must retain empty arrays over HTTP so
// the center can distinguish an explicitly empty result from missing input.
func (v Review) MarshalJSON() ([]byte, error) {
	type plain Review
	b, err := json.Marshal(plain(v))
	if err != nil {
		return nil, err
	}
	if v.RulesDigest == "" {
		return b, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(b, &object); err != nil {
		return nil, err
	}
	object["issues"], err = json.Marshal(v.Issues)
	if err != nil {
		return nil, err
	}
	object["resolutions"], err = json.Marshal(v.Resolutions)
	if err != nil {
		return nil, err
	}
	return json.Marshal(object)
}
