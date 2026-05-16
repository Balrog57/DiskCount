package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

type SettingMeta struct {
	Key             string
	Label           string
	Secret          bool
	RestartRequired bool
	Default         string
}

var AppSettings = []SettingMeta{
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
	{"IDEALO_PAGE_URLS", "Idealo page URLs", false, true, ""},
	{"LEDENICHEUR_FEED_URLS", "leDenicheur feed URLs", false, true, ""},
	{"LEDENICHEUR_PAGE_URLS", "leDenicheur page URLs", false, true, ""},
	{"LEBONCOIN_FEED_URLS", "leboncoin feed URLs", false, true, ""},
	{"KEEPA_API_KEY", "Keepa API key", true, true, ""},
	{"KEEPA_ASINS", "Keepa ASINs", false, true, ""},
	{"EBAY_CLIENT_ID", "eBay client ID", false, true, ""},
	{"EBAY_CLIENT_SECRET", "eBay client secret", true, true, ""},
	{"EBAY_SEARCH_QUERIES", "eBay search queries", false, true, ""},
	{"SOURCE_HEADLESS_FALLBACK", "Headless fallback", false, true, "true"},
	{"BYPARR_URL", "Byparr URL", false, true, "http://byparr:8191"},
	{"NOTIFICATION_PRICE_DROP_PCT", "Notification price drop percent", false, true, "2.0"},
	{"TELEGRAM_MESSAGE_DELAY_SECONDS", "Telegram message delay seconds", false, true, "0.5"},
	{"SCRAPE_INTERVAL_CRON", "Scan interval", false, true, "@every 4h"},
	{"AMAZON_TLDS", "Amazon TLDs", false, true, "fr,de"},
	{"LDLC_ENABLED", "LDLC enabled", false, true, "true"},
	{"ALTERNATE_TLDS", "Alternate TLDs", false, true, "fr,de"},
	{"GEIZHALS_ENABLED", "Geizhals enabled", false, true, "true"},
	{"RDC_ENABLED", "Rue du Commerce enabled", false, true, "true"},
	{"PCPART_ENABLED", "PCPartPicker enabled", false, true, "true"},
}

type Config struct {
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
	IdealoPageURLs           []string
	LeDenicheurFeedURLs      []string
	LeDenicheurPageURLs      []string
	LeBonCoinFeedURLs        []string
	KeepaAPIKey              string
	KeepaASINs               []string
	EbayClientID             string
	EbayClientSecret         string
	EbaySearchQueries        []string
	HeadlessFallback         bool
	ByparrURL                string
	NotificationPriceDropPct float64
	TelegramMessageDelayS    float64
	ScrapeIntervalCron       string
	AmazonTLDs               []string
	LDLCEnabled              bool
	AlternateTLDs            []string
	GeizhalsEnabled          bool
	RDCEnabled               bool
	PCPartEnabled            bool
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
		IdealoPageURLs:        splitCSV(values["IDEALO_PAGE_URLS"]),
		LeDenicheurFeedURLs:   splitCSV(values["LEDENICHEUR_FEED_URLS"]),
		LeDenicheurPageURLs:   splitCSV(values["LEDENICHEUR_PAGE_URLS"]),
		LeBonCoinFeedURLs:     splitCSV(values["LEBONCOIN_FEED_URLS"]),
		KeepaAPIKey:           values["KEEPA_API_KEY"],
		KeepaASINs:            splitCSV(values["KEEPA_ASINS"]),
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
		AmazonTLDs:         splitCSV(values["AMAZON_TLDS"]),
		LDLCEnabled:        parseBool(values["LDLC_ENABLED"], true),
		AlternateTLDs:      splitCSV(values["ALTERNATE_TLDS"]),
		GeizhalsEnabled:    parseBool(values["GEIZHALS_ENABLED"], true),
		RDCEnabled:         parseBool(values["RDC_ENABLED"], true),
		PCPartEnabled:      parseBool(values["PCPART_ENABLED"], true),
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
