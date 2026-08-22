package fireworksdsv4

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/fantasy"
)

var dsv4ModelPattern = regexp.MustCompile(`(?i)(?:^|/)deepseek-v4(?:[-./]|$)`)

// Option configures the Fireworks DSV4 provider.
type Option func(*provider)

type provider struct {
	baseURL         string
	apiKey          string
	headers         map[string]string
	extraBody       map[string]any
	defaultEffort   string
	maxOutputTokens int64
	httpClient      *http.Client
}

// New creates a native Fantasy provider for Fireworks-hosted DSV4 models.
func New(options ...Option) (fantasy.Provider, error) {
	p := &provider{
		headers:    make(map[string]string),
		extraBody:  make(map[string]any),
		httpClient: http.DefaultClient,
	}
	for _, option := range options {
		option(p)
	}
	if strings.TrimSpace(p.baseURL) == "" {
		return nil, fmt.Errorf("fireworks DSV4 base URL is required")
	}
	return p, nil
}

// WithBaseURL configures the Fireworks inference endpoint.
func WithBaseURL(value string) Option {
	return func(p *provider) { p.baseURL = value }
}

// WithAPIKey configures Fireworks bearer authentication.
func WithAPIKey(value string) Option {
	return func(p *provider) { p.apiKey = value }
}

// WithHeaders configures provider-level request headers.
func WithHeaders(value map[string]string) Option {
	return func(p *provider) { p.headers = maps.Clone(value) }
}

// WithExtraBody configures additional unowned completion fields.
func WithExtraBody(value map[string]any) Option {
	return func(p *provider) { p.extraBody = maps.Clone(value) }
}

// WithDefaultReasoningEffort configures the model's default effort.
func WithDefaultReasoningEffort(value string) Option {
	return func(p *provider) { p.defaultEffort = value }
}

// WithMaxOutputTokens caps completion output at the model's catalog limit.
func WithMaxOutputTokens(value int64) Option {
	return func(p *provider) { p.maxOutputTokens = value }
}

// WithHTTPClient overrides the HTTP client, primarily for debug logging and
// tests.
func WithHTTPClient(value *http.Client) Option {
	return func(p *provider) {
		if value != nil {
			p.httpClient = value
		}
	}
}

func (*provider) Name() string { return Name }

func (p *provider) LanguageModel(_ context.Context, modelID string) (fantasy.LanguageModel, error) {
	if !IsModelID(modelID) {
		return nil, fmt.Errorf("fireworks DSV4 provider does not support model %q", modelID)
	}
	return &languageModel{
		provider:        Name,
		modelID:         modelID,
		baseURL:         p.baseURL,
		apiKey:          p.apiKey,
		headers:         maps.Clone(p.headers),
		extraBody:       maps.Clone(p.extraBody),
		defaultEffort:   p.defaultEffort,
		maxOutputTokens: p.maxOutputTokens,
		httpClient:      p.httpClient,
	}, nil
}

// IsModelID reports whether a Fireworks model ID belongs to the DSV4 family.
func IsModelID(modelID string) bool {
	return dsv4ModelPattern.MatchString(modelID)
}

// CatalogAlias derives the bundled constrained provider from Fireworks'
// current catalog so prices and limits stay synchronized with Catwalk.
func CatalogAlias(source catwalk.Provider) (catwalk.Provider, bool) {
	if source.ID != catwalk.InferenceProviderFireworks {
		return catwalk.Provider{}, false
	}
	models := make([]catwalk.Model, 0, len(source.Models))
	for _, model := range source.Models {
		if !IsModelID(model.ID) {
			continue
		}
		model.ReasoningLevels = []string{"none", "low", "high", "max"}
		model.DefaultReasoningEffort = "high"
		model.CanReason = true
		model.SupportsImages = false
		models = append(models, model)
	}
	if len(models) == 0 {
		return catwalk.Provider{}, false
	}
	alias := source
	alias.ID = catwalk.InferenceProvider(Name)
	alias.Name = DisplayName
	alias.Type = catwalk.Type(Name)
	alias.Models = models
	alias.DefaultHeaders = maps.Clone(source.DefaultHeaders)
	alias.DefaultLargeModelID = catalogDefault(models, source.DefaultLargeModelID, "pro")
	alias.DefaultSmallModelID = catalogDefault(models, source.DefaultSmallModelID, "flash")
	return alias, true
}

func catalogDefault(models []catwalk.Model, preferred, fallbackFragment string) string {
	if slices.ContainsFunc(models, func(model catwalk.Model) bool { return model.ID == preferred }) {
		return preferred
	}
	for _, model := range models {
		if strings.Contains(strings.ToLower(model.ID), fallbackFragment) {
			return model.ID
		}
	}
	return models[0].ID
}
