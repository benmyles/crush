package fireworksdsv4

import (
	"encoding/json"

	"charm.land/fantasy"
)

const (
	typeProviderOptions = Name + ".options"
	typeMetadata        = Name + ".metadata"
)

// ProviderOptions controls Fireworks DSV4 calls.
type ProviderOptions struct {
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	ExtraBody       map[string]any `json:"extra_body,omitempty"`
}

// Metadata records provider response identifiers that do not fit Fantasy's
// common response fields.
type Metadata struct {
	ResponseID      string `json:"response_id,omitempty"`
	RawFinishReason string `json:"raw_finish_reason,omitempty"`
}

func init() {
	fantasy.RegisterProviderType(typeProviderOptions, func(data []byte) (fantasy.ProviderOptionsData, error) {
		var value ProviderOptions
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		return &value, nil
	})
	fantasy.RegisterProviderType(typeMetadata, func(data []byte) (fantasy.ProviderOptionsData, error) {
		var value Metadata
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		return &value, nil
	})
}

func (*ProviderOptions) Options() {}

func (o ProviderOptions) MarshalJSON() ([]byte, error) {
	type plain ProviderOptions
	return fantasy.MarshalProviderType(typeProviderOptions, plain(o))
}

func (o *ProviderOptions) UnmarshalJSON(data []byte) error {
	type plain ProviderOptions
	var value plain
	if err := fantasy.UnmarshalProviderType(data, &value); err != nil {
		return err
	}
	*o = ProviderOptions(value)
	return nil
}

func (*Metadata) Options() {}

func (m Metadata) MarshalJSON() ([]byte, error) {
	type plain Metadata
	return fantasy.MarshalProviderType(typeMetadata, plain(m))
}

func (m *Metadata) UnmarshalJSON(data []byte) error {
	type plain Metadata
	var value plain
	if err := fantasy.UnmarshalProviderType(data, &value); err != nil {
		return err
	}
	*m = Metadata(value)
	return nil
}

// ParseOptions converts configuration maps into typed Fantasy options.
func ParseOptions(data map[string]any) (*ProviderOptions, error) {
	var options ProviderOptions
	if err := fantasy.ParseOptions(data, &options); err != nil {
		return nil, err
	}
	return &options, nil
}
