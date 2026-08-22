package config

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/scraper"
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
	{"DISCORD_BOT_TOKEN", "Discord bot token", true, true, ""},
	{"DISCORD_CHANNEL_ID", "Discord channel ID", false, true, ""},
	{"REQUEST_TIMEOUT_SECONDS", "Request timeout seconds", false, true, "30"},
	{"USER_AGENT", "HTTP user agent", false, true, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"},
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
	{"EBAY_MARKETPLACES", "eBay marketplaces (CSV: EBAY_FR, EBAY_DE, EBAY_IT, EBAY_ES)", false, true, "EBAY_FR"},
	{"KEEPA_DOMAINS", "Keepa domains (CSV: 3=DE, 4=FR, 6=IT, 7=ES)", false, true, ""},
	{"MINDFACTORY_URLS", "Mindfactory URLs (CSV)", false, true, ""},
	{"ALTERNATE_URLS", "Alternate France URLs (CSV)", false, true, "https://www.alternate.fr/listing.xhtml?q=hdd,https://www.alternate.fr/listing.xhtml?q=ssd"},
	{"COMPUTERUNIVERSE_URLS", "Computeruniverse URLs (CSV)", false, true, ""},
	{"PROSHOP_URLS", "ProShop URLs (CSV)", false, true, ""},
	{"GEIZHALS_URLS", "Geizhals URLs (CSV)", false, true, ""},
	{"LDLC_URLS", "LDLC URLs (CSV)", false, true, "https://www.ldlc.com/recherche/disque+dur/"},
	{"TOPACHAT_URLS", "TopAchat URLs (CSV)", false, true, "https://www.topachat.com/pages/produits_cat_est_micro_puis_rubrique_est_w_ssd.html"},
	{"GROSBILL_URLS", "Grosbill URLs (CSV)", false, true, "https://www.grosbill.com/disque-ssd-49.aspx,https://www.grosbill.com/disque-dur-3-5-interne-3.aspx"},
	{"FNAC_URLS", "Fnac/Darty URLs (CSV)", false, true, "https://www.fnac.com/Disques-durs-internes/s21528,https://www.fnac.com/SSD/s41228"},
	{"BOULANGER_URLS", "Boulanger URLs (CSV)", false, true, "https://www.boulanger.com/c/disque-dur-interne,https://www.boulanger.com/c/ssd"},
	{"CDISCOUNT_URLS", "Cdiscount URLs (CSV)", false, true, "https://www.cdiscount.com/informatique/disques-durs-internes/l-1072201.html,https://www.cdiscount.com/informatique/ssd/l-1072208.html"},
	{"RAKUTEN_URLS", "Rakuten FR URLs (CSV)", false, true, ""},
	{"RUEDUCOMMERCE_URLS", "Rue du Commerce URLs (CSV)", false, true, "https://www.rueducommerce.fr/recherche/disque-dur-interne/"},
	{"BACKMARKET_URLS", "Back Market URLs (CSV)", false, true, ""},
	{"DARTY_URLS", "Darty storage URLs (CSV)", false, true, "https://www.darty.com/nav/q/informatique-disque-dur-ssd.html"},
	{"MATERIEL_URLS", "Materiel.net storage URLs (CSV)", false, true, "https://www.materiel.net/disque-dur-interne/l430/,https://www.materiel.net/disque-ssd/l429/"},
	{"CYBERTEK_URLS", "Cybertek storage URLs (CSV)", false, true, "https://www.cybertek.fr/disque-ssd-49.aspx,https://www.cybertek.fr/disque-dur-3-5-interne-3.aspx"},
	{"CORSAIR_URLS", "Corsair France storage URLs (CSV)", false, true, "https://www.corsair.com/fr/fr/c/data-storage"},
	{"PCCOMPONENTES_URLS", "PCComponentes France storage URLs (CSV)", false, true, "https://www.pccomponentes.fr/categories/disques-durs/disque-ssd"},
	{"TOPBIZ_URLS", "Topbiz storage URLs (CSV)", false, true, "https://www.topbiz.fr/95-disques-durs-ssd,https://www.topbiz.fr/96-disques-durs-internes"},
	{"SOURCE_HEADLESS_FALLBACK", "Headless fallback", false, true, "true"},
	{"BYPARR_URL", "Byparr URL", false, true, "http://byparr:8191"},
	{"NOTIFICATION_PRICE_DROP_PCT", "Notification price drop percent", false, true, "2.0"},
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
	{"SOURCE_HEALTH_STREAK_THRESHOLD", "Consecutive zero-deal scans before a source is flagged", false, false, "3"},
	{"BACK_IN_STOCK_HOURS", "Hours of absence after which a returning deal is flagged as back-in-stock", false, false, "48"},
}

type Config struct {
	WebAdminPassword         string
	DiscordBotToken          string
	DiscordChannelID         string
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
	EbayMarketplaces         []string
	KeepaDomains             []int
	MindfactoryURLs          []string
	AlternateURLs            []string
	ComputeruniverseURLs     []string
	ProshopURLs              []string
	GeizhalsURLs             []string
	LDLCURLs                 []string
	TopachatURLs             []string
	GrosbillURLs             []string
	FnacURLs                 []string
	BoulangerURLs            []string
	CdiscountURLs            []string
	RakutenURLs              []string
	RueDuCommerceURLs        []string
	BackmarketURLs           []string
	DartyURLs                []string
	MaterielURLs             []string
	CybertekURLs             []string
	CorsairURLs              []string
	PCComponentesURLs        []string
	TopbizURLs               []string
	HeadlessFallback         bool
	ByparrURL                string
	NotificationPriceDropPct float64
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
	SourceHealthThreshold    int
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

	return loadConfig(values)
}

func loadConfig(values map[string]string) *Config {
	cfg := &Config{
		WebAdminPassword:      values["WEB_ADMIN_PASSWORD"],
		DiscordBotToken:       values["DISCORD_BOT_TOKEN"],
		DiscordChannelID:      values["DISCORD_CHANNEL_ID"],
		DatabaseURL:           value(values, "DATABASE_URL", "postgres://localhost:5432/diskcount"),
		WebAdminAddr:          value(values, "WEB_ADMIN_ADDR", "0.0.0.0:47832"),
		RequestTimeoutSeconds: parseFloat(values["REQUEST_TIMEOUT_SECONDS"], 30),
		UserAgent:             value(values, "USER_AGENT", scraper.DefaultUserAgent),
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
		EbayMarketplaces:      splitCSV(values["EBAY_MARKETPLACES"]),
		KeepaDomains:          resolveKeepaDomains(values),
		MindfactoryURLs:       splitCSV(values["MINDFACTORY_URLS"]),
		AlternateURLs:         splitCSV(values["ALTERNATE_URLS"]),
		ComputeruniverseURLs:  splitCSV(values["COMPUTERUNIVERSE_URLS"]),
		ProshopURLs:           splitCSV(values["PROSHOP_URLS"]),
		GeizhalsURLs:          splitCSV(values["GEIZHALS_URLS"]),
		LDLCURLs:              splitCSV(values["LDLC_URLS"]),
		TopachatURLs:          splitCSV(values["TOPACHAT_URLS"]),
		GrosbillURLs:          splitCSV(values["GROSBILL_URLS"]),
		FnacURLs:              splitCSV(values["FNAC_URLS"]),
		BoulangerURLs:         splitCSV(values["BOULANGER_URLS"]),
		CdiscountURLs:         splitCSV(values["CDISCOUNT_URLS"]),
		RakutenURLs:           splitCSV(values["RAKUTEN_URLS"]),
		RueDuCommerceURLs:     splitCSV(values["RUEDUCOMMERCE_URLS"]),
		BackmarketURLs:        splitCSV(values["BACKMARKET_URLS"]),
		DartyURLs:             splitCSV(values["DARTY_URLS"]),
		MaterielURLs:          splitCSV(values["MATERIEL_URLS"]),
		CybertekURLs:          splitCSV(values["CYBERTEK_URLS"]),
		CorsairURLs:           splitCSV(values["CORSAIR_URLS"]),
		PCComponentesURLs:     splitCSV(values["PCCOMPONENTES_URLS"]),
		TopbizURLs:            splitCSV(values["TOPBIZ_URLS"]),
		HeadlessFallback:      parseBool(values["SOURCE_HEADLESS_FALLBACK"], true),
		ByparrURL:             value(values, "BYPARR_URL", "http://byparr:8191"),
		NotificationPriceDropPct: parseFloat(
			values["NOTIFICATION_PRICE_DROP_PCT"], 2,
		),
		ScrapeIntervalCron:       value(values, "SCRAPE_INTERVAL_CRON", "@every 4h"),
		RetryMaxAttempts:         int(parseFloat(values["RETRY_MAX_ATTEMPTS"], 3)),
		RetryBaseDelaySeconds:    parseFloat(values["RETRY_BASE_DELAY_SECONDS"], 0.5),
		RetryMaxDelaySeconds:     parseFloat(values["RETRY_MAX_DELAY_SECONDS"], 30),
		EnabledSources:           splitCSV(values["ENABLED_SOURCES"]),
		HeadersExtra:             values["HEADERS_EXTRA"],
		UserAgentPool:            splitCSV(values["USER_AGENT_POOL"]),
		BlockedDetectionKeywords: splitCSV(values["BLOCKED_DETECTION_KEYWORDS"]),
		CircuitBreakerEnabled:    parseBool(values["CIRCUIT_BREAKER_ENABLED"], true),
		CircuitBreakerThreshold:  int(parseFloat(values["CIRCUIT_BREAKER_THRESHOLD"], 5)),
		CircuitBreakerTimeoutS:   parseFloat(values["CIRCUIT_BREAKER_TIMEOUT_SECONDS"], 60),
		PerRequestTimeoutSeconds: parseFloat(values["PER_REQUEST_TIMEOUT_SECONDS"], 10),
		SourceHealthThreshold:    int(parseFloat(values["SOURCE_HEALTH_STREAK_THRESHOLD"], 3)),
		BackInStockHours:         parseFloat(values["BACK_IN_STOCK_HOURS"], 48),
	}

	// Warn about Keepa multi-domain timeout risk: the scanner wraps each
	// source call with context.WithTimeout(ctx, RequestTimeoutSeconds).
	// Keepa issues N_ASINs × M_domains requests at ~800ms each, so large
	// multi-domain configs need a generously long REQUEST_TIMEOUT_SECONDS.
	// 50 ASINs × 4 domains × 0.8s = 160s > default 30s → guaranteed timeout.
	if len(cfg.KeepaDomains) > 2 && cfg.RequestTimeoutSeconds < 120 {
		estimated := float64(len(cfg.KeepaASINs)*len(cfg.KeepaDomains)) * 0.8
		slog.Warn("keepa multi-domaine: REQUEST_TIMEOUT_SECONDS risque de timeout",
			"domains", len(cfg.KeepaDomains),
			"asins", len(cfg.KeepaASINs),
			"estime_s", estimated,
			"timeout_s", cfg.RequestTimeoutSeconds,
			"hint", "augmenter REQUEST_TIMEOUT_SECONDS >= 120 ou reduire KEEPA_DOMAINS",
		)
	}
	return cfg
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

func splitInts(s string) []int {
	if s == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil || v <= 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

func resolveKeepaDomains(values map[string]string) []int {
	domains := splitInts(values["KEEPA_DOMAINS"])
	if len(domains) > 0 {
		return domains
	}
	// backward-compat: single KEEPA_DOMAIN value
	if v := values["KEEPA_DOMAIN"]; v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			return []int{d}
		}
	}
	return []int{4}
}

func (c *Config) Validate() []error {
	var errs []error
	if (c.DiscordBotToken == "") != (c.DiscordChannelID == "") {
		errs = append(errs, fmt.Errorf("DISCORD_BOT_TOKEN and DISCORD_CHANNEL_ID must be configured together"))
	}
	if c.DiscordChannelID != "" {
		if _, err := strconv.ParseUint(c.DiscordChannelID, 10, 64); err != nil {
			errs = append(errs, fmt.Errorf("DISCORD_CHANNEL_ID must be numeric"))
		}
	}
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
