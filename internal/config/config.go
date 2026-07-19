package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type SettingMeta struct {
	Key             string
	Label           string
	Secret          bool
	RestartRequired bool
	Default         string
}

var AppSettings = []SettingMeta{
	{"WEB_ADMIN_PASSWORD", "Web admin password", true, true, ""},
	{"TELEGRAM_BOT_TOKEN", "Telegram bot token", true, true, ""},
	{"REQUEST_TIMEOUT_SECONDS", "Request timeout seconds", false, true, "30"},
	{"USER_AGENT", "HTTP user agent", false, true, "DiskCountBot/2.0"},
	{"DISKPRICES_URL", "DiskPrices URL", false, true, "https://diskprices.com/?locale=fr"},
	{"PRICEPERGIG_ENABLED", "PricePerGig enabled", false, true, "true"},
	{"PRICEPERGIG_API_URL", "PricePerGig API URL", false, true, "https://api.pricepergig.com/drives"},
	{"PRICEPERGIG_MARKET", "PricePerGig market", false, true, "amazon.fr"},
	{"PRICEPERTB_URLS", "PricePerTB URLs", false, true, "https://pricepertb.com/fr"},
	{"DEALABS_RSS_URLS", "Dealabs RSS URLs", false, true, ""},
	{"IDEALO_FEED_URLS", "Idealo feed URLs", false, true, ""},
	{"LEDENICHEUR_FEED_URLS", "leDenicheur feed URLs", false, true, ""},
	{"LEBONCOIN_FEED_URLS", "leboncoin feed URLs", false, true, ""},
	{"KEEPA_API_KEY", "Keepa API key", true, true, ""},
	{"KEEPA_ASINS", "Keepa ASINs", false, true, ""},
	{"KEEPA_DOMAIN", "Keepa Amazon domain (1=com,2=uk,3=de,4=fr)", false, true, "4"},
	{"EBAY_CLIENT_ID", "eBay client ID", false, true, ""},
	{"EBAY_CLIENT_SECRET", "eBay client secret", true, true, ""},
	{"EBAY_SEARCH_QUERIES", "eBay search queries", false, true, ""},
	{"SOURCE_HEADLESS_FALLBACK", "Headless fallback", false, true, "true"},
	{"BYPARR_URL", "Byparr URL", false, true, "http://byparr:8191"},
	{"NOTIFICATION_PRICE_DROP_PCT", "Notification price drop percent", false, true, "2.0"},
	{"TELEGRAM_MESSAGE_DELAY_SECONDS", "Telegram message delay seconds", false, true, "0.5"},
	{"SCRAPE_INTERVAL_CRON", "Scan interval", false, true, "@every 4h"},
	{"RETRY_MAX_ATTEMPTS", "Retry max attempts", false, false, "3"},
	{"RETRY_BASE_DELAY_SECONDS", "Retry base delay seconds", false, false, "0.5"},
	{"RETRY_MAX_DELAY_SECONDS", "Retry max delay seconds", false, false, "30"},
	{"ENABLED_SOURCES", "Enabled sources (comma-separated)", false, true, ""},
	{"HEADERS_EXTRA", "Extra HTTP headers (JSON)", false, true, ""},
	{"USER_AGENT_POOL", "User agent pool (comma-separated)", false, true, ""},
	{"BLOCKED_DETECTION_KEYWORDS", "Blocked detection keywords", false, false, "cf-browser-verification,Just a moment...,Checking your browser,Enable JavaScript,Access Denied,captcha,403 Forbidden"},
	{"CIRCUIT_BREAKER_ENABLED", "Circuit breaker enabled", false, false, "true"},
	{"CIRCUIT_BREAKER_THRESHOLD", "Circuit breaker failure threshold", false, false, "5"},
	{"CIRCUIT_BREAKER_TIMEOUT_SECONDS", "Circuit breaker open timeout seconds", false, false, "60"},
	{"PER_REQUEST_TIMEOUT_SECONDS", "Per-request timeout seconds", false, false, "10"},
	{"TELEGRAM_ADMIN_CHAT_ID", "Telegram admin chat ID for source health alerts", false, false, ""},
	{"SOURCE_HEALTH_STREAK_THRESHOLD", "Consecutive zero-deal scans before a source is flagged", false, false, "3"},
	{"SOURCE_HEALTH_NOTIFY", "Notify admin via Telegram when a source is flagged", false, false, "true"},
	{"ADMIN_LOCALE", "Locale for admin-facing notifications (fr|en)", false, false, "fr"},
	{"BACK_IN_STOCK_HOURS", "Hours of absence after which a returning deal is flagged as back-in-stock", false, false, "48"},
}

type Config struct {
	WebAdminPassword         string
	TelegramBotToken         string
	DatabaseURL              string
	WebAdminAddr             string
	RequestTimeoutSeconds    float64
	UserAgent                string
	DiskPricesURL            string
	PricePerGigEnabled       bool
	PricePerGigAPIURL        string
	PricePerGigMarket        string
	PricePerTBURLs           []string
	DealabsRSSURLs           []string
	IdealoFeedURLs           []string
	LeDenicheurFeedURLs      []string
	LeBonCoinFeedURLs        []string
	KeepaAPIKey              string
	KeepaASINs               []string
	KeepaDomain              int
	EbayClientID             string
	EbayClientSecret         string
	EbaySearchQueries        []string
	HeadlessFallback         bool
	ByparrURL                string
	NotificationPriceDropPct float64
	TelegramMessageDelayS    float64
	ScrapeIntervalCron       string
	RetryMaxAttempts         int
	RetryBaseDelaySeconds    float64
	RetryMaxDelaySeconds     float64
	EnabledSources           []string
	HeadersExtra             string
	UserAgentPool            []string
	BlockedDetectionKeywords []string
	CircuitBreakerEnabled    bool
	CircuitBreakerThreshold  int
	CircuitBreakerTimeoutS   float64
	PerRequestTimeoutSeconds float64
	TelegramAdminChatID      string
	SourceHealthThreshold    int
	SourceHealthNotify       bool
	AdminLocale              string
	BackInStockHours         float64
}

func LoadBootstrap() *Config {
	return LoadWithAppValues(nil)
}

func LoadWithAppValues(appValues map[string]string) *Config {
	values := DefaultValues()
	for k, v := range ReadEnvFile(".env") {
		values[k] = v
	}
	for _, env := range os.Environ() {
		k, v, ok := strings.Cut(env, "=")
		if ok {
			values[k] = v
		}
	}
	for k, v := range appValues {
		values[k] = v
	}

	return &Config{
		WebAdminPassword:      values["WEB_ADMIN_PASSWORD"],
		TelegramBotToken:      values["TELEGRAM_BOT_TOKEN"],
		DatabaseURL:           value(values, "DATABASE_URL", "postgres://localhost:5432/diskcount"),
		WebAdminAddr:          value(values, "WEB_ADMIN_ADDR", "0.0.0.0:47832"),
		RequestTimeoutSeconds: parseFloat(values["REQUEST_TIMEOUT_SECONDS"], 30),
		UserAgent:             value(values, "USER_AGENT", "DiskCountBot/2.0"),
		DiskPricesURL:         values["DISKPRICES_URL"],
		PricePerGigEnabled:    parseBool(values["PRICEPERGIG_ENABLED"], true),
		PricePerGigAPIURL:     values["PRICEPERGIG_API_URL"],
		PricePerGigMarket:     value(values, "PRICEPERGIG_MARKET", "amazon.fr"),
		PricePerTBURLs:        splitCSV(values["PRICEPERTB_URLS"]),
		DealabsRSSURLs:        splitCSV(values["DEALABS_RSS_URLS"]),
		IdealoFeedURLs:        splitCSV(values["IDEALO_FEED_URLS"]),
		LeDenicheurFeedURLs:   splitCSV(values["LEDENICHEUR_FEED_URLS"]),
		LeBonCoinFeedURLs:     splitCSV(values["LEBONCOIN_FEED_URLS"]),
		KeepaAPIKey:           values["KEEPA_API_KEY"],
		KeepaASINs:            splitCSV(values["KEEPA_ASINS"]),
		KeepaDomain:           int(parseFloat(values["KEEPA_DOMAIN"], 4)),
		EbayClientID:          values["EBAY_CLIENT_ID"],
		EbayClientSecret:      values["EBAY_CLIENT_SECRET"],
		EbaySearchQueries:     splitCSV(values["EBAY_SEARCH_QUERIES"]),
		HeadlessFallback:      parseBool(values["SOURCE_HEADLESS_FALLBACK"], true),
		ByparrURL:             value(values, "BYPARR_URL", "http://byparr:8191"),
		NotificationPriceDropPct: parseFloat(
			values["NOTIFICATION_PRICE_DROP_PCT"], 2,
		),
		TelegramMessageDelayS: parseFloat(
			values["TELEGRAM_MESSAGE_DELAY_SECONDS"], 0.5,
		),
		ScrapeIntervalCron: value(values, "SCRAPE_INTERVAL_CRON", "@every 4h"),
		RetryMaxAttempts:            int(parseFloat(values["RETRY_MAX_ATTEMPTS"], 3)),
		RetryBaseDelaySeconds:       parseFloat(values["RETRY_BASE_DELAY_SECONDS"], 0.5),
		RetryMaxDelaySeconds:        parseFloat(values["RETRY_MAX_DELAY_SECONDS"], 30),
		EnabledSources:              splitCSV(values["ENABLED_SOURCES"]),
		HeadersExtra:                values["HEADERS_EXTRA"],
		UserAgentPool:               splitCSV(values["USER_AGENT_POOL"]),
		BlockedDetectionKeywords:    splitCSV(values["BLOCKED_DETECTION_KEYWORDS"]),
		CircuitBreakerEnabled:       parseBool(values["CIRCUIT_BREAKER_ENABLED"], true),
		CircuitBreakerThreshold:     int(parseFloat(values["CIRCUIT_BREAKER_THRESHOLD"], 5)),
		CircuitBreakerTimeoutS:      parseFloat(values["CIRCUIT_BREAKER_TIMEOUT_SECONDS"], 60),
		PerRequestTimeoutSeconds:    parseFloat(values["PER_REQUEST_TIMEOUT_SECONDS"], 10),
		TelegramAdminChatID:         values["TELEGRAM_ADMIN_CHAT_ID"],
		SourceHealthThreshold:       int(parseFloat(values["SOURCE_HEALTH_STREAK_THRESHOLD"], 3)),
		SourceHealthNotify:          parseBool(values["SOURCE_HEALTH_NOTIFY"], true),
		AdminLocale:                 values["ADMIN_LOCALE"],
		BackInStockHours:            parseFloat(values["BACK_IN_STOCK_HOURS"], 48),
	}
}

func DefaultValues() map[string]string {
	values := make(map[string]string, len(AppSettings))
	for _, meta := range AppSettings {
		values[meta.Key] = meta.Default
	}
	return values
}

func ImportableEnvValues() map[string]string {
	values := make(map[string]string)
	for k, v := range ReadEnvFile(".env") {
		if IsAppSetting(k) {
			values[k] = v
		}
	}
	for _, env := range os.Environ() {
		k, v, ok := strings.Cut(env, "=")
		if ok && IsAppSetting(k) {
			values[k] = v
		}
	}
	return values
}

func ReadEnvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if key != "" {
			values[key] = val
		}
	}
	return values
}

func IsAppSetting(key string) bool {
	for _, meta := range AppSettings {
		if meta.Key == key {
			return true
		}
	}
	return false
}

func SecretKeys() map[string]bool {
	out := make(map[string]bool)
	for _, meta := range AppSettings {
		if meta.Secret {
			out[meta.Key] = true
		}
	}
	return out
}

func value(values map[string]string, key, fallback string) string {
	if val, ok := values[key]; ok {
		return val
	}
	return fallback
}

func parseFloat(raw string, fallback float64) float64 {
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return val
}

func parseBool(raw string, fallback bool) bool {
	if raw == "" {
		return fallback
	}
	val, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return val
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) Validate() []error {
	var errs []error
	if c.RequestTimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("REQUEST_TIMEOUT_SECONDS must be positive"))
	}
	if c.PerRequestTimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("PER_REQUEST_TIMEOUT_SECONDS must be positive"))
	}
	if c.PerRequestTimeoutSeconds > c.RequestTimeoutSeconds {
		errs = append(errs, fmt.Errorf("PER_REQUEST_TIMEOUT_SECONDS (%v) must be <= REQUEST_TIMEOUT_SECONDS (%v)", c.PerRequestTimeoutSeconds, c.RequestTimeoutSeconds))
	}
	if c.RetryMaxAttempts < 0 {
		errs = append(errs, fmt.Errorf("RETRY_MAX_ATTEMPTS must be >= 0"))
	}
	if c.RetryBaseDelaySeconds < 0 {
		errs = append(errs, fmt.Errorf("RETRY_BASE_DELAY_SECONDS must be >= 0"))
	}
	if c.RetryMaxDelaySeconds < 0 {
		errs = append(errs, fmt.Errorf("RETRY_MAX_DELAY_SECONDS must be >= 0"))
	}
	if c.ByparrURL != "" && !isValidURL(c.ByparrURL) {
		errs = append(errs, fmt.Errorf("BYPARR_URL is not a valid URL: %s", c.ByparrURL))
	}
	if c.CircuitBreakerThreshold < 1 {
		errs = append(errs, fmt.Errorf("CIRCUIT_BREAKER_THRESHOLD must be >= 1"))
	}
	if c.CircuitBreakerTimeoutS <= 0 {
		errs = append(errs, fmt.Errorf("CIRCUIT_BREAKER_TIMEOUT_SECONDS must be positive"))
	}
	if c.NotificationPriceDropPct < 0 {
		errs = append(errs, fmt.Errorf("NOTIFICATION_PRICE_DROP_PCT must be >= 0"))
	}
	for _, u := range c.DiskPricesURLUrls() {
		if !isValidURL(u) {
			errs = append(errs, fmt.Errorf("invalid DISKPRICES_URL: %s", u))
		}
	}
	return errs
}

func (c *Config) DiskPricesURLUrls() []string {
	if c.DiskPricesURL == "" {
		return nil
	}
	return []string{c.DiskPricesURL}
}

// IsSourceEnabled returns true when the source name is allowed by
// ENABLED_SOURCES. An empty list (the default) means "all sources are
// enabled" — the feature is opt-in.
func (c *Config) IsSourceEnabled(name string) bool {
	if len(c.EnabledSources) == 0 {
		return true
	}
	for _, s := range c.EnabledSources {
		if s == name {
			return true
		}
	}
	return false
}

func isValidURL(raw string) bool {
	if raw == "" {
		return false
	}
	_, err := url.Parse(raw)
	return err == nil
}

// ParseScrapeInterval is the single source of truth for interpreting the
// SCRAPE_INTERVAL_CRON setting. The scheduler (scanner.ScheduleLoop) and the
// web dashboard ("prochain scan" countdown) both call it so they agree on
// the cadence.
//
// Currently only the "@every <duration>" form is supported (e.g.
// "@every 4h", "@every 30m"). Real cron expressions are rejected (ok=false)
// rather than silently downgraded to a default — the previous behaviour
// silently turned any unrecognised spec into a 4h interval, which made it
// impossible to tell whether a custom cadence was being honoured.
func ParseScrapeInterval(spec string) (time.Duration, bool) {
	spec = strings.TrimSpace(spec)
	after, ok := strings.CutPrefix(spec, "@every ")
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSpace(after))
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// DefaultScrapeInterval is the fallback used by the scheduler when the
// configured spec cannot be parsed. It matches AppSettings["SCRAPE_INTERVAL_CRON"].
const DefaultScrapeInterval = 4 * time.Hour
