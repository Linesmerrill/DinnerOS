package httpx

import "encoding/json"

// Optional is a request field that tells an absent key from null: Set is
// false when the key is missing, and Value is nil when it was null. Use it
// where "leave it alone" and "clear it" both need saying, such as a price.
type Optional[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON implements json.Unmarshaler. encoding/json calls it only for
// keys that are present, null included.
func (o *Optional[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}
