package growth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"pentagi/pkg/database"
	"pentagi/pkg/growth/screening"
	"pentagi/pkg/system"
)

// ErrModelList means the endpoint did not return its model list; the page then takes a
// typed model name.
var ErrModelList = errors.New("the model list could not be fetched")

type ModelOption struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

const modelListTimeout = 15 * time.Second

var versionSegment = regexp.MustCompile(`^v\d+$`)

// ListModels returns the models a provider's endpoint serves, for a choice on the
// settings page before anything is saved. An empty value is the saved setting's when it
// has the same provider, else the .env one's.
func (s *Service) ListModels(ctx context.Context, userID int64, kind, provider string, values map[string]string) ([]ModelOption, error) {
	spec, ok := settingSpecs[kind]
	if !ok || (kind != SettingLLM && kind != SettingScreening && kind != SettingEmbedding) {
		return nil, fmt.Errorf("%w: %q has no model list", ErrInvalidSetting, kind)
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = spec.defaultProvider
	}
	if !slices.Contains(spec.providers, provider) {
		return nil, fmt.Errorf("%w: provider of %s must be one of %s", ErrInvalidSetting, kind, strings.Join(spec.providers, ", "))
	}

	v := make(map[string]string, len(values))
	for k, val := range values {
		v[k] = strings.TrimSpace(val)
	}
	fallback := map[string]string{}
	if row, err := s.db.GetUserGrowthSetting(ctx, database.GetUserGrowthSettingParams{UserID: userID, Kind: kind}); err == nil {
		if stored, err := s.open(row); err == nil && stored.Provider == provider {
			fallback = stored.Values
		}
	}
	if len(fallback) == 0 {
		if p, env := envValues(s.cfg, kind); p == provider {
			fallback = env
		}
	}
	for k, val := range fallback {
		if v[k] == "" {
			v[k] = val
		}
	}

	var (
		endpoint, key = "", v["api_key"]
		listURL       string
		anthropic     bool
		keep          = func(string) bool { return true }
	)
	switch kind {
	case SettingEmbedding:
		endpoint = v["server_url"]
		if key == "" {
			key = s.reusedEmbeddingKey(ctx, userID, provider)
		}
		switch provider {
		case embeddingOpenRouter:
			if endpoint == "" {
				endpoint = OpenRouterEmbeddingURL
			}
			listURL = strings.TrimRight(endpoint, "/") + "/embeddings/models"
		case "openai":
			if endpoint == "" {
				endpoint = "https://api.openai.com/v1"
			}
			keep = func(id string) bool { return strings.Contains(id, "embedding") }
		default:
			return nil, fmt.Errorf("%w: %s embedding models are typed by name", ErrInvalidSetting, provider)
		}
	case SettingScreening:
		endpoint = v["api_url"]
		switch provider {
		case screeningOpenRouter:
			if endpoint == "" {
				endpoint = screening.OpenRouterURL
			}
			keep = func(id string) bool { return strings.HasPrefix(id, screening.OpenRouterModelPrefix) }
		case screeningTypeSafe:
			if endpoint == "" {
				endpoint = screening.DefaultURL
			}
		}
	case SettingLLM:
		if provider == "gemini" {
			return nil, fmt.Errorf("%w: gemini models are typed by name", ErrInvalidSetting)
		}
		endpoint = v["server_url"]
		if endpoint == "" {
			_, defURL := llmTargets[provider](s.cfg)
			endpoint = *defURL
		}
		anthropic = provider == "anthropic" || v["protocol"] == llmProtocolAnthropic
	}
	if endpoint == "" || !isHTTPURL(endpoint) {
		return nil, fmt.Errorf("%w: the endpoint URL is missing or not an http(s) URL", ErrInvalidSetting)
	}
	if key == "" {
		return nil, fmt.Errorf("%w: the model list needs the API key", ErrInvalidSetting)
	}

	if listURL == "" {
		listURL = modelsURL(endpoint)
	}
	models, err := s.fetchModels(ctx, listURL, key, anthropic)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrModelList, err)
	}
	out := models[:0]
	for _, m := range models {
		if keep(m.ID) {
			out = append(out, m)
		}
	}
	slices.SortFunc(out, func(a, b ModelOption) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

// reusedEmbeddingKey is the key the embedder runs with when the embedding setting gives
// none: the LLM's OpenAI key, or its OpenRouter key.
func (s *Service) reusedEmbeddingKey(ctx context.Context, userID int64, provider string) string {
	cfg, err := s.ConfigFor(ctx, userID)
	if err != nil {
		return ""
	}
	switch {
	case provider == embeddingOpenRouter && isOpenRouter(cfg.EmbeddingURL):
		return cfg.EmbeddingKey
	case provider == "openai" && cfg.EmbeddingProvider == "openai" && !isOpenRouter(cfg.EmbeddingURL):
		if cfg.EmbeddingKey != "" {
			return cfg.EmbeddingKey
		}
		return cfg.OpenAIKey
	}
	return ""
}

// modelsURL appends the OpenAI-style /models to a base that ends in its API version,
// and /v1/models to one that does not.
func modelsURL(base string) string {
	u, _ := url.Parse(base)
	path := strings.TrimRight(u.Path, "/")
	if versionSegment.MatchString(path[strings.LastIndex(path, "/")+1:]) {
		u.Path = path + "/models"
	} else {
		u.Path = path + "/v1/models"
	}
	return u.String()
}

func (s *Service) fetchModels(ctx context.Context, endpoint, key string, anthropic bool) ([]ModelOption, error) {
	ctx, cancel := context.WithTimeout(ctx, modelListTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if anthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: modelListTimeout}
	if c, err := system.GetHTTPClient(s.cfg); err == nil {
		client = c
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d: %s", endpoint, resp.StatusCode, strings.TrimSpace(truncateRunes(string(data), 200)))
	}
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, fmt.Errorf("%s did not answer with a model list: %v", endpoint, err)
	}
	out := make([]ModelOption, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID == "" {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.DisplayName
		}
		out = append(out, ModelOption{ID: m.ID, Name: name})
	}
	return out, nil
}
