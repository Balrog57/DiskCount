package config

import (
	"fmt"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

var (
	instance *Config
	once     sync.Once
)

type Config struct {
	TelegramBotToken         string
	TelegramAdminUserIDs     []int64
	DatabaseURL              string
	PollIntervalSeconds      int
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

func Get() *Config {
	once.Do(func() { instance = load() })
	return instance
}

func load() *Config {
	v := viper.New()
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	_ = v.ReadInConfig()
	v.AutomaticEnv()
	return &Config{
		TelegramBotToken:         getStr(v, "TELEGRAM_BOT_TOKEN"),
		TelegramAdminUserIDs:     splitI64(getStr(v, "TELEGRAM_ADMIN_USER_IDS")),
		DatabaseURL:              getStr(v, "DATABASE_URL", "postgres://diskcount:diskcount@localhost:5432/diskcount"),
		RequestTimeoutSeconds:    getFl(v, "REQUEST_TIMEOUT_SECONDS", 30),
		UserAgent:                getStr(v, "USER_AGENT", "DiskCountBot/2.0"),
		DiskPricesURL:            getStr(v, "DISKPRICES_URL", "https://diskprices.com/?locale=fr"),
		PricePerGigEnabled:       v.GetBool("PRICEPERGIG_ENABLED"),
		PricePerGigAPIURL:        getStr(v, "PRICEPERGIG_API_URL", "https://api.pricepergig.com/drives"),
		PricePerGigMarket:        getStr(v, "PRICEPERGIG_MARKET", "amazon.fr"),
		PricePerTBURLs:           splitCSV(getStr(v, "PRICEPERTB_URLS", "https://pricepertb.com/fr")),
		DealabsRSSURLs:           splitCSV(getStr(v, "DEALABS_RSS_URLS")),
		IdealoFeedURLs:           splitCSV(getStr(v, "IDEALO_FEED_URLS")),
		IdealoPageURLs:           splitCSV(getStr(v, "IDEALO_PAGE_URLS")),
		LeDenicheurFeedURLs:      splitCSV(getStr(v, "LEDENICHEUR_FEED_URLS")),
		LeDenicheurPageURLs:      splitCSV(getStr(v, "LEDENICHEUR_PAGE_URLS")),
		LeBonCoinFeedURLs:        splitCSV(getStr(v, "LEBONCOIN_FEED_URLS")),
		KeepaAPIKey:              getStr(v, "KEEPA_API_KEY"),
		KeepaASINs:               splitCSV(getStr(v, "KEEPA_ASINS")),
		EbayClientID:             getStr(v, "EBAY_CLIENT_ID"),
		EbayClientSecret:         getStr(v, "EBAY_CLIENT_SECRET"),
		EbaySearchQueries:        splitCSV(getStr(v, "EBAY_SEARCH_QUERIES")),
		HeadlessFallback:         v.GetBool("SOURCE_HEADLESS_FALLBACK"),
		ByparrURL:                getStr(v, "BYPARR_URL", "http://byparr:8191"),
		NotificationPriceDropPct: getFl(v, "NOTIFICATION_PRICE_DROP_PCT", 2),
		TelegramMessageDelayS:    getFl(v, "TELEGRAM_MESSAGE_DELAY_SECONDS", 0.5),
		ScrapeIntervalCron:       getStr(v, "SCRAPE_INTERVAL_CRON", "@every 4h"),
		AmazonTLDs:               splitCSV(getStr(v, "AMAZON_TLDS")),
		LDLCEnabled:              v.GetBool("LDLC_ENABLED"),
		AlternateTLDs:            splitCSV(getStr(v, "ALTERNATE_TLDS")),
		GeizhalsEnabled:          v.GetBool("GEIZHALS_ENABLED"),
		RDCEnabled:               v.GetBool("RDC_ENABLED"),
		PCPartEnabled:            v.GetBool("PCPART_ENABLED"),
	}
}

func getStr(v *viper.Viper, key string, fb ...string) string {
	val := v.GetString(key)
	if val == "" && len(fb) > 0 {
		return fb[0]
	}
	return val
}

func getFl(v *viper.Viper, key string, fb float64) float64 {
	val := v.GetFloat64(key)
	if val == 0 {
		return fb
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

func splitI64(s string) []int64 {
	parts := splitCSV(s)
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		var n int64
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil && n != 0 {
			out = append(out, n)
		}
	}
	return out
}
