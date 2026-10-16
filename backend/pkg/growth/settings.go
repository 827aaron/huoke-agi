package growth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/growth/connectors"
	"pentagi/pkg/system"

	"github.com/sirupsen/logrus"
)

const (
	SettingLLM       = "llm"
	SettingTelegram  = "telegram"
	SettingYouTube   = "youtube"
	SettingWebSearch = "web_search"
	SettingEmail     = "email"
	SettingProxy     = "proxy"

	SettingStatusUntested  = "untested"
	SettingStatusConnected = "connected"
	SettingStatusInvalid   = "invalid"

	SettingSourceDatabase = "database"
	SettingSourceEnv      = "env"
	SettingSourceNone     = "none"
)

var (
	ErrNoSecretKey      = errors.New("GROWTH_SECRET_KEY is not set, so credentials cannot be stored encrypted")
	ErrInvalidSetting   = errors.New("invalid setting")
	ErrSettingNotStored = errors.New("no saved setting of this kind")
)

type settingField struct {
	name     string
	secret   bool
	required bool
	// list fields take comma or newline separated items and are stored comma-joined.
	list bool
	// mask replaces maskSecret for a secret whose shape the page needs to show.
	mask func(string) string
}

type settingSpec struct {
	providers []string
	fields    []settingField
	// apply copies the stored values onto cfg; values hold every field, empty when unset.
	apply func(cfg *config.Config, provider string, values map[string]string)
}

// A custom endpoint speaks OpenAI Chat Completions, which PentAGI's custom provider
// serves, or Anthropic Messages, which its anthropic provider serves at another URL.
const (
	llmProtocolOpenAI    = "openai_chat"
	llmProtocolAnthropic = "anthropic_messages"
)

// llmTargets maps a provider to its key and server URL in Config. "custom" is the
// OpenAI-compatible endpoint of LLM_SERVER_*, and the only one that takes a model.
var llmTargets = map[string]func(c *config.Config) (key, url *string){
	"openai":    func(c *config.Config) (*string, *string) { return &c.OpenAIKey, &c.OpenAIServerURL },
	"anthropic": func(c *config.Config) (*string, *string) { return &c.AnthropicAPIKey, &c.AnthropicServerURL },
	"gemini":    func(c *config.Config) (*string, *string) { return &c.GeminiAPIKey, &c.GeminiServerURL },
	"deepseek":  func(c *config.Config) (*string, *string) { return &c.DeepSeekAPIKey, &c.DeepSeekServerURL },
	"glm":       func(c *config.Config) (*string, *string) { return &c.GLMAPIKey, &c.GLMServerURL },
	"kimi":      func(c *config.Config) (*string, *string) { return &c.KimiAPIKey, &c.KimiServerURL },
	"qwen":      func(c *config.Config) (*string, *string) { return &c.QwenAPIKey, &c.QwenServerURL },
	"minimax":   func(c *config.Config) (*string, *string) { return &c.MiniMaxAPIKey, &c.MiniMaxServerURL },
	"mistral":   func(c *config.Config) (*string, *string) { return &c.MistralAPIKey, &c.MistralServerURL },
	"xai":       func(c *config.Config) (*string, *string) { return &c.XAIAPIKey, &c.XAIServerURL },
	"custom":    func(c *config.Config) (*string, *string) { return &c.LLMServerKey, &c.LLMServerURL },
}

var searchTargets = map[string]func(c *config.Config) *string{
	"tavily":     func(c *config.Config) *string { return &c.TavilyAPIKey },
	"google":     func(c *config.Config) *string { return &c.GoogleAPIKey },
	"perplexity": func(c *config.Config) *string { return &c.PerplexityAPIKey },
	"firecrawl":  func(c *config.Config) *string { return &c.FirecrawlAPIKey },
	"traversaal": func(c *config.Config) *string { return &c.TraversaalAPIKey },
}

var settingSpecs = map[string]settingSpec{
	SettingLLM: {
		providers: sortedKeys(llmTargets),
		fields: []settingField{
			{name: "api_key", secret: true, required: true},
			{name: "server_url"},
			{name: "model"},
			{name: "protocol"},
		},
		apply: func(c *config.Config, provider string, v map[string]string) {
			if provider == "custom" && v["protocol"] == llmProtocolAnthropic {
				provider = "anthropic"
			}
			key, serverURL := llmTargets[provider](c)
			*key = v["api_key"]
			if v["server_url"] != "" {
				*serverURL = v["server_url"]
			}
			if provider == "custom" && v["model"] != "" {
				c.LLMServerModel = v["model"]
			}
		},
	},
	SettingTelegram: {
		fields: []settingField{
			{name: "api_id", required: true},
			{name: "api_hash", secret: true, required: true},
		},
		apply: func(c *config.Config, _ string, v map[string]string) {
			c.TGAPIID, _ = strconv.Atoi(v["api_id"])
			c.TGAPIHash = v["api_hash"]
		},
	},
	SettingYouTube: {
		fields: []settingField{{name: "api_keys", secret: true, required: true, list: true}},
		apply: func(c *config.Config, _ string, v map[string]string) {
			c.YouTubeAPIKey = v["api_keys"]
		},
	},
	SettingWebSearch: {
		providers: sortedKeys(searchTargets),
		fields: []settingField{
			{name: "api_keys", secret: true, required: true, list: true},
			{name: "cx"},
		},
		apply: func(c *config.Config, provider string, v map[string]string) {
			*searchTargets[provider](c) = v["api_keys"]
			if provider == "google" && v["cx"] != "" {
				c.GoogleCXKey = v["cx"]
			}
		},
	},
	// notify_emails is a comma-separated list; empty means the owner's login address.
	SettingEmail: {
		fields: []settingField{
			{name: "host", required: true},
			{name: "port"},
			{name: "username"},
			{name: "password", secret: true},
			{name: "from", required: true},
			{name: "tls"},
			{name: "notify_emails"},
		},
		apply: func(c *config.Config, _ string, v map[string]string) {
			c.SMTPHost = v["host"]
			if port, err := strconv.Atoi(v["port"]); err == nil {
				c.SMTPPort = port
			}
			c.SMTPUsername = v["username"]
			c.SMTPPassword = v["password"]
			c.SMTPFrom = v["from"]
			if v["tls"] != "" {
				c.SMTPTLS = v["tls"]
			}
		},
	},
	// The default proxy of the customer's platform accounts; an account's own proxy wins.
	SettingProxy: {
		fields: []settingField{{name: "url", secret: true, required: true, mask: maskEndpoint}},
		apply: func(c *config.Config, _ string, v map[string]string) {
			c.GrowthProxyURL = v["url"]
		},
	},
}

var settingKinds = []string{SettingLLM, SettingTelegram, SettingYouTube, SettingWebSearch, SettingEmail, SettingProxy}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// SettingView is a credential as the settings page shows it: secret fields masked to
// their last four characters. Source says where the running value comes from.
type SettingView struct {
	Kind         string            `json:"kind"`
	Provider     string            `json:"provider,omitempty"`
	Providers    []string          `json:"providers,omitempty"`
	Fields       map[string]string `json:"fields"`
	Source       string            `json:"source"`
	Status       string            `json:"status"`
	StatusReason string            `json:"status_reason,omitempty"`
	TestedAt     *time.Time        `json:"tested_at,omitempty"`
	UpdatedAt    *time.Time        `json:"updated_at,omitempty"`
}

type storedSetting struct {
	Provider string            `json:"provider"`
	Values   map[string]string `json:"values"`
}

func (s *Service) sealer() (cipher.AEAD, error) {
	if strings.TrimSpace(s.cfg.GrowthSecretKey) == "" {
		return nil, ErrNoSecretKey
	}
	key := sha256.Sum256([]byte(s.cfg.GrowthSecretKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// The row's owner and kind are authenticated with the ciphertext, so a blob copied
// into another customer's row or another kind fails to open.
func settingAAD(userID int64, kind string) []byte {
	return []byte(fmt.Sprintf("growth_settings:%d:%s", userID, kind))
}

func (s *Service) seal(userID int64, kind string, v storedSetting) ([]byte, error) {
	plain, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return s.sealBytes(plain, settingAAD(userID, kind))
}

func (s *Service) sealBytes(plain, aad []byte) ([]byte, error) {
	aead, err := s.sealer()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize(), aead.NonceSize()+len(plain)+aead.Overhead())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, aad), nil
}

func (s *Service) openBytes(secret, aad []byte) ([]byte, error) {
	aead, err := s.sealer()
	if err != nil {
		return nil, err
	}
	if len(secret) < aead.NonceSize() {
		return nil, errors.New("stored credential is truncated")
	}
	return aead.Open(nil, secret[:aead.NonceSize()], secret[aead.NonceSize():], aad)
}

func (s *Service) open(row database.GrowthSetting) (storedSetting, error) {
	var v storedSetting
	plain, err := s.openBytes(row.Secret, settingAAD(row.UserID, row.Kind))
	switch {
	case errors.Is(err, ErrNoSecretKey):
		return v, err
	case err != nil:
		return v, fmt.Errorf("stored %s credential does not open with the current GROWTH_SECRET_KEY", row.Kind)
	}
	if err := json.Unmarshal(plain, &v); err != nil {
		return v, fmt.Errorf("stored %s credential is corrupt: %w", row.Kind, err)
	}
	return v, nil
}

// SaveSetting stores one kind of credential for a customer. A field left empty keeps the
// value saved before under the same provider, so the page can resubmit a masked key.
func (s *Service) SaveSetting(
	ctx context.Context,
	userID int64,
	kind, provider string,
	values map[string]string,
) (SettingView, error) {
	spec, ok := settingSpecs[kind]
	if !ok {
		return SettingView{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidSetting, kind)
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if len(spec.providers) == 0 {
		provider = ""
	} else if !slices.Contains(spec.providers, provider) {
		return SettingView{}, fmt.Errorf("%w: provider of %s must be one of %s",
			ErrInvalidSetting, kind, strings.Join(spec.providers, ", "))
	}
	if _, err := s.sealer(); err != nil {
		return SettingView{}, err
	}

	merged := make(map[string]string, len(spec.fields))
	if prev, err := s.db.GetUserGrowthSetting(ctx, database.GetUserGrowthSettingParams{UserID: userID, Kind: kind}); err == nil {
		if old, err := s.open(prev); err == nil && old.Provider == provider {
			for k, v := range old.Values {
				merged[k] = v
			}
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return SettingView{}, fmt.Errorf("failed to load %s setting: %w", kind, err)
	}

	for name, value := range values {
		if !slices.ContainsFunc(spec.fields, func(f settingField) bool { return f.name == name }) {
			return SettingView{}, fmt.Errorf("%w: %s has no field %q", ErrInvalidSetting, kind, name)
		}
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if spec.fields[slices.IndexFunc(spec.fields, func(f settingField) bool { return f.name == name })].list {
			value = strings.Join(config.SplitList(value), ",")
		}
		merged[name] = value
	}
	for _, f := range spec.fields {
		if f.required && merged[f.name] == "" {
			return SettingView{}, fmt.Errorf("%w: %s needs %s", ErrInvalidSetting, kind, f.name)
		}
	}
	if err := validateSetting(kind, provider, merged); err != nil {
		return SettingView{}, err
	}

	secret, err := s.seal(userID, kind, storedSetting{Provider: provider, Values: merged})
	if err != nil {
		return SettingView{}, fmt.Errorf("failed to encrypt %s setting: %w", kind, err)
	}
	row, err := s.db.UpsertGrowthSetting(ctx, database.UpsertGrowthSettingParams{
		UserID: userID, Kind: kind, Provider: provider, Secret: secret,
	})
	if err != nil {
		return SettingView{}, fmt.Errorf("failed to store %s setting: %w", kind, err)
	}
	if kind == SettingTelegram || kind == SettingProxy {
		s.dropUserConnectors(ctx, userID)
	}
	return settingView(row, spec, provider, merged), nil
}

func validateSetting(kind, provider string, v map[string]string) error {
	switch kind {
	case SettingTelegram:
		if id, err := strconv.Atoi(v["api_id"]); err != nil || id <= 0 {
			return fmt.Errorf("%w: api_id must be the positive number from my.telegram.org", ErrInvalidSetting)
		}
		if !telegramHash.MatchString(v["api_hash"]) {
			return fmt.Errorf("%w: api_hash must be the 32-character hex string from my.telegram.org", ErrInvalidSetting)
		}
	case SettingWebSearch:
		if provider == "google" && v["cx"] == "" {
			return fmt.Errorf("%w: google search needs cx, the search engine id", ErrInvalidSetting)
		}
	case SettingEmail:
		if p := v["port"]; p != "" {
			if n, err := strconv.Atoi(p); err != nil || n <= 0 || n > 65535 {
				return fmt.Errorf("%w: port must be a number between 1 and 65535", ErrInvalidSetting)
			}
		}
		if t := v["tls"]; t != "" && !slices.Contains([]string{smtpStartTLS, smtpImplicitTLS, smtpNoTLS}, t) {
			return fmt.Errorf("%w: tls must be %s, %s or %s", ErrInvalidSetting, smtpStartTLS, smtpImplicitTLS, smtpNoTLS)
		}
		if _, err := mail.ParseAddress(v["from"]); err != nil {
			return fmt.Errorf("%w: from is not an email address: %v", ErrInvalidSetting, err)
		}
		if _, err := parseRecipients(v["notify_emails"]); err != nil {
			return fmt.Errorf("%w: notify_emails: %v", ErrInvalidSetting, err)
		}
	case SettingProxy:
		if _, err := parseProxyURL(v["url"]); err != nil {
			return err
		}
	case SettingLLM:
		if p := v["protocol"]; p != "" && (provider != "custom" || (p != llmProtocolOpenAI && p != llmProtocolAnthropic)) {
			return fmt.Errorf("%w: protocol applies to custom only and is %s or %s",
				ErrInvalidSetting, llmProtocolOpenAI, llmProtocolAnthropic)
		}
		if provider == "custom" && v["server_url"] == "" {
			return fmt.Errorf("%w: a custom endpoint needs server_url", ErrInvalidSetting)
		}
	}
	if raw := v["server_url"]; raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: server_url must be an http(s) URL", ErrInvalidSetting)
		}
	}
	return nil
}

var telegramHash = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func (s *Service) dropUserConnectors(ctx context.Context, userID int64) {
	accounts, err := s.db.GetUserChannelAccounts(ctx, userID)
	if err != nil {
		return
	}
	for _, a := range accounts {
		s.DropConnector(a.ID)
	}
}

func (s *Service) DeleteSetting(ctx context.Context, userID int64, kind string) error {
	n, err := s.db.DeleteUserGrowthSetting(ctx, database.DeleteUserGrowthSettingParams{UserID: userID, Kind: kind})
	if err != nil {
		return fmt.Errorf("failed to delete %s setting: %w", kind, err)
	}
	if n == 0 {
		return ErrSettingNotStored
	}
	if kind == SettingTelegram || kind == SettingProxy {
		s.dropUserConnectors(ctx, userID)
	}
	return nil
}

// ListSettings returns every kind, saved or not; a kind not saved shows what .env provides.
func (s *Service) ListSettings(ctx context.Context, userID int64) ([]SettingView, error) {
	rows, err := s.db.GetUserGrowthSettings(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	byKind := make(map[string]database.GrowthSetting, len(rows))
	for _, r := range rows {
		byKind[r.Kind] = r
	}

	views := make([]SettingView, 0, len(settingKinds))
	for _, kind := range settingKinds {
		spec := settingSpecs[kind]
		row, saved := byKind[kind]
		if !saved {
			views = append(views, s.envView(kind, spec))
			continue
		}
		stored, err := s.open(row)
		if err != nil {
			v := settingView(row, spec, row.Provider, nil)
			v.Status, v.StatusReason = SettingStatusInvalid, err.Error()
			views = append(views, v)
			continue
		}
		views = append(views, settingView(row, spec, stored.Provider, stored.Values))
	}
	return views, nil
}

func settingView(row database.GrowthSetting, spec settingSpec, provider string, values map[string]string) SettingView {
	v := SettingView{
		Kind:         row.Kind,
		Provider:     provider,
		Providers:    spec.providers,
		Fields:       maskFields(spec, values),
		Source:       SettingSourceDatabase,
		Status:       row.Status,
		StatusReason: row.StatusReason,
		TestedAt:     nullTime(row.TestedAt.Time, row.TestedAt.Valid),
		UpdatedAt:    nullTime(row.UpdatedAt.Time, row.UpdatedAt.Valid),
	}
	return v
}

func maskFields(spec settingSpec, values map[string]string) map[string]string {
	out := make(map[string]string, len(spec.fields))
	for _, f := range spec.fields {
		v := values[f.name]
		switch {
		case !f.secret || v == "":
		case f.mask != nil:
			v = f.mask(v)
		case f.list:
			items := config.SplitList(v)
			for i, item := range items {
				items[i] = maskSecret(item)
			}
			v = strings.Join(items, ",")
		default:
			v = maskSecret(v)
		}
		out[f.name] = v
	}
	return out
}

func maskSecret(v string) string {
	r := []rune(v)
	if len(r) < 8 {
		return "••••"
	}
	return "••••" + string(r[len(r)-4:])
}

// envView reports a kind the customer has not saved: the provider .env configures, if any.
func (s *Service) envView(kind string, spec settingSpec) SettingView {
	v := SettingView{Kind: kind, Providers: spec.providers, Source: SettingSourceNone, Status: SettingStatusUntested}
	provider, values := envValues(s.cfg, kind)
	v.Fields = maskFields(spec, values)
	if values["api_key"] != "" || values["api_keys"] != "" || values["api_hash"] != "" ||
		values["host"] != "" || values["url"] != "" {
		v.Source, v.Provider = SettingSourceEnv, provider
	}
	return v
}

// envValues reads one kind from cfg in the stored-setting shape; for a kind with
// providers it takes the first configured one in name order.
func envValues(cfg *config.Config, kind string) (string, map[string]string) {
	switch kind {
	case SettingLLM:
		for _, p := range sortedKeys(llmTargets) {
			key, serverURL := llmTargets[p](cfg)
			if *key != "" {
				v := map[string]string{"api_key": *key, "server_url": *serverURL}
				if p == "custom" {
					v["model"] = cfg.LLMServerModel
				}
				return p, v
			}
		}
	case SettingTelegram:
		if cfg.TGAPIHash != "" {
			return "", map[string]string{"api_id": strconv.Itoa(cfg.TGAPIID), "api_hash": cfg.TGAPIHash}
		}
	case SettingYouTube:
		return "", map[string]string{"api_keys": strings.Join(cfg.YouTubeAPIKeys(), ",")}
	case SettingProxy:
		return "", map[string]string{"url": cfg.GrowthProxyURL}
	case SettingWebSearch:
		for _, p := range sortedKeys(searchTargets) {
			if key := *searchTargets[p](cfg); key != "" {
				return p, map[string]string{"api_keys": strings.Join(config.SplitList(key), ","), "cx": cfg.GoogleCXKey}
			}
		}
	case SettingEmail:
		if cfg.SMTPHost != "" {
			return "", map[string]string{
				"host": cfg.SMTPHost, "port": strconv.Itoa(cfg.SMTPPort), "username": cfg.SMTPUsername,
				"password": cfg.SMTPPassword, "from": cfg.SMTPFrom, "tls": cfg.SMTPTLS,
			}
		}
	}
	return "", map[string]string{}
}

// ConfigFor returns the configuration a customer's work runs with: each saved credential
// replaces the matching .env value, everything else is the process configuration. With
// nothing saved it returns the process configuration itself, not a copy. A credential
// that does not open (the master key changed) is skipped and logged, so .env applies.
func (s *Service) ConfigFor(ctx context.Context, userID int64) (*config.Config, error) {
	rows, err := s.db.GetUserGrowthSettings(ctx, userID)
	if err != nil {
		return s.cfg, fmt.Errorf("failed to load settings of user %d: %w", userID, err)
	}
	if len(rows) == 0 {
		return s.cfg, nil
	}

	cfg := *s.cfg
	applied := false
	for _, row := range rows {
		spec, ok := settingSpecs[row.Kind]
		if !ok {
			continue
		}
		stored, err := s.open(row)
		if err != nil {
			logrus.WithContext(ctx).WithError(err).WithField("user_id", userID).Warn("ignoring saved growth credential")
			continue
		}
		if len(spec.providers) > 0 && !slices.Contains(spec.providers, stored.Provider) {
			continue
		}
		spec.apply(&cfg, stored.Provider, stored.Values)
		applied = true
	}
	if !applied {
		return s.cfg, nil
	}
	return &cfg, nil
}

// Probe endpoints that Config does not carry; variables so tests can point them at a stub.
var (
	youTubeAPIURL    = "https://www.googleapis.com/youtube/v3"
	tavilyAPIURL     = "https://api.tavily.com"
	googleSearchURL  = "https://www.googleapis.com/customsearch/v1"
	perplexityAPIURL = "https://api.perplexity.ai"
)

// TestSetting checks the credential a customer's work would run with (saved, else .env)
// against the platform with the cheapest authenticated call it offers, and records the
// verdict on the saved row. "untested" means no check exists without a login.
func (s *Service) TestSetting(ctx context.Context, userID int64, kind string) (SettingView, error) {
	spec, ok := settingSpecs[kind]
	if !ok {
		return SettingView{}, fmt.Errorf("%w: unknown kind %q", ErrInvalidSetting, kind)
	}

	row, err := s.db.GetUserGrowthSetting(ctx, database.GetUserGrowthSettingParams{UserID: userID, Kind: kind})
	saved := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SettingView{}, fmt.Errorf("failed to load %s setting: %w", kind, err)
	}

	var (
		provider string
		values   map[string]string
		view     SettingView
	)
	if saved {
		stored, err := s.open(row)
		if err != nil {
			return s.recordTest(ctx, row, spec, "", nil, SettingStatusInvalid, err.Error())
		}
		provider, values = stored.Provider, stored.Values
	} else {
		view = s.envView(kind, spec)
		if view.Source == SettingSourceNone {
			view.Status, view.StatusReason = SettingStatusInvalid, "nothing is configured for "+kind
			return view, nil
		}
		provider, values = envValues(s.cfg, kind)
	}

	cfg := *s.cfg
	spec.apply(&cfg, provider, values)
	probed := provider
	if kind == SettingLLM && provider == "custom" && values["protocol"] == llmProtocolAnthropic {
		probed = "anthropic"
	}
	status, reason := s.probe(ctx, &cfg, kind, probed)

	if !saved {
		view.Status, view.StatusReason = status, reason
		return view, nil
	}
	return s.recordTest(ctx, row, spec, provider, values, status, reason)
}

func (s *Service) recordTest(
	ctx context.Context,
	row database.GrowthSetting,
	spec settingSpec,
	provider string,
	values map[string]string,
	status, reason string,
) (SettingView, error) {
	updated, err := s.db.UpdateGrowthSettingStatus(ctx, database.UpdateGrowthSettingStatusParams{
		UserID: row.UserID, Kind: row.Kind, Status: status, StatusReason: reason,
	})
	if err != nil {
		return SettingView{}, fmt.Errorf("failed to record test of %s setting: %w", row.Kind, err)
	}
	if provider == "" {
		provider = row.Provider
	}
	return settingView(updated, spec, provider, values), nil
}

func (s *Service) probe(ctx context.Context, cfg *config.Config, kind, provider string) (string, string) {
	if kind == SettingEmail {
		if err := checkSMTP(ctx, cfg); err != nil {
			return SettingStatusInvalid, err.Error()
		}
		return SettingStatusConnected, ""
	}
	if kind == SettingTelegram {
		check, ok := connectors.LookupCredentialCheck(connectors.PlatformTelegram)
		if !ok {
			return SettingStatusUntested, "the format is valid; Telegram confirms the pair only at the first login"
		}
		if err := check(ctx, cfg); err != nil {
			return SettingStatusInvalid, err.Error()
		}
		return SettingStatusConnected, ""
	}
	if kind == SettingProxy {
		latency, err := checkProxy(ctx, cfg, cfg.GrowthProxyURL)
		if err != nil {
			return SettingStatusInvalid, err.Error()
		}
		return SettingStatusConnected, fmt.Sprintf("reached the internet in %d ms", latency.Milliseconds())
	}
	switch kind {
	case SettingYouTube:
		return s.probeKeys(ctx, cfg, kind, provider, func(c *config.Config) *string { return &c.YouTubeAPIKey })
	case SettingWebSearch:
		if target, ok := searchTargets[provider]; ok {
			return s.probeKeys(ctx, cfg, kind, provider, target)
		}
	}
	return s.probeHTTP(ctx, cfg, kind, provider)
}

// probeKeys checks every key of a list. A key whose quota is spent still counts as
// valid: it works again once the quota resets.
func (s *Service) probeKeys(
	ctx context.Context,
	cfg *config.Config,
	kind, provider string,
	target func(c *config.Config) *string,
) (string, string) {
	var bad, spent []string
	for _, key := range config.SplitList(*target(cfg)) {
		one := *cfg
		*target(&one) = key
		status, reason := s.probeHTTP(ctx, &one, kind, provider)
		switch {
		case status == SettingStatusConnected:
		case isQuotaVerdict(reason):
			spent = append(spent, maskSecret(key))
		default:
			bad = append(bad, maskSecret(key)+": "+reason)
		}
	}
	if len(bad) > 0 {
		return SettingStatusInvalid, strings.Join(bad, "; ")
	}
	if len(spent) > 0 {
		return SettingStatusConnected, "quota spent for today: " + strings.Join(spent, ", ")
	}
	return SettingStatusConnected, ""
}

// isQuotaVerdict tells a spent quota or rate limit from a rejected key.
func isQuotaVerdict(reason string) bool {
	return strings.Contains(reason, "quotaExceeded") || strings.Contains(reason, " answered 429:") ||
		strings.Contains(reason, " answered 432:") || strings.Contains(reason, " answered 433:")
}

func (s *Service) probeHTTP(ctx context.Context, cfg *config.Config, kind, provider string) (string, string) {
	req, err := probeRequest(ctx, cfg, kind, provider)
	if err != nil {
		return SettingStatusInvalid, err.Error()
	}
	if req == nil {
		return SettingStatusUntested, "no check exists for " + provider
	}

	client, err := system.GetHTTPClient(cfg)
	if err != nil {
		return SettingStatusInvalid, err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return SettingStatusInvalid, "unreachable: " + err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return SettingStatusConnected, ""
	}
	// Google puts the reason deep in its error body, past what the verdict keeps.
	if bytes.Contains(body, []byte(`"quotaExceeded"`)) || bytes.Contains(body, []byte(`"dailyLimitExceeded"`)) {
		return SettingStatusInvalid, fmt.Sprintf("%s answered %d: quotaExceeded", req.URL.Host, resp.StatusCode)
	}
	return SettingStatusInvalid, fmt.Sprintf("%s answered %d: %s", req.URL.Host, resp.StatusCode,
		truncateRunes(strings.TrimSpace(string(body)), 300))
}

// probeRequest builds the check for a credential, or nil when the service has no
// side-effect-free call to make. Keys go in headers where the API allows it, so they
// stay out of proxy and server logs.
func probeRequest(ctx context.Context, cfg *config.Config, kind, provider string) (*http.Request, error) {
	get := func(rawURL string) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	}
	post := func(rawURL string, body any) (*http.Request, error) {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(data))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, err
	}

	switch kind {
	case SettingLLM:
		key, serverURL := llmTargets[provider](cfg)
		base := strings.TrimRight(*serverURL, "/")
		switch provider {
		case "anthropic":
			req, err := get(base + "/models")
			if err == nil {
				req.Header.Set("x-api-key", *key)
				req.Header.Set("anthropic-version", "2023-06-01")
			}
			return req, err
		case "gemini":
			req, err := get(base + "/v1beta/models")
			if err == nil {
				req.Header.Set("x-goog-api-key", *key)
			}
			return req, err
		default:
			req, err := get(base + "/models")
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+*key)
			}
			return req, err
		}

	case SettingYouTube:
		q := url.Values{"part": {"snippet"}, "regionCode": {"US"}}
		req, err := get(youTubeAPIURL + "/videoCategories?" + q.Encode())
		if err == nil {
			req.Header.Set("x-goog-api-key", cfg.YouTubeAPIKey)
		}
		return req, err

	case SettingWebSearch:
		switch provider {
		case "tavily":
			req, err := post(tavilyAPIURL+"/search", map[string]any{"query": "game", "max_results": 1})
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+cfg.TavilyAPIKey)
			}
			return req, err
		case "google":
			q := url.Values{"cx": {cfg.GoogleCXKey}, "q": {"game"}, "num": {"1"}}
			req, err := get(googleSearchURL + "?" + q.Encode())
			if err == nil {
				req.Header.Set("x-goog-api-key", cfg.GoogleAPIKey)
			}
			return req, err
		case "perplexity":
			req, err := get(perplexityAPIURL + "/models")
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+cfg.PerplexityAPIKey)
			}
			return req, err
		case "firecrawl":
			req, err := get(strings.TrimRight(cfg.FirecrawlAPIURL, "/") + "/v1/team/credit-usage")
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+cfg.FirecrawlAPIKey)
			}
			return req, err
		}
	}
	return nil, nil
}
