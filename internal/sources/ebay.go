package sources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Balrog57/DiskCount/internal/domain"
	"github.com/Balrog57/DiskCount/internal/parsing"
	"github.com/Balrog57/DiskCount/internal/scraper"
)

func init() {
	Register(func(r *Registry) Source {
		cfg := r.Config()
		if cfg.EbayClientID == "" || cfg.EbayClientSecret == "" || len(cfg.EbaySearchQueries) == 0 {
			return nil
		}
		return &Ebay{
			http:      r.HTTP(),
			clientID:  cfg.EbayClientID,
			secret:    cfg.EbayClientSecret,
			queries:   cfg.EbaySearchQueries,
			apiBase:   "https://api.ebay.com",
			oauthBase: "https://api.ebay.com/identity/v1/oauth2/token",
		}
	})
}

// Ebay implements the eBay Browse API source. It authenticates via the OAuth2
// client-credentials grant and searches for configured queries, mapping each
// item summary to a domain.Deal.
type Ebay struct {
	http      scraper.Fetcher
	clientID  string
	secret    string
	queries   []string
	apiBase   string
	oauthBase string

	mu          sync.Mutex
	token       string
	tokenExpiry time.Time
}

func (s *Ebay) Name() string { return "ebay" }

func (s *Ebay) Info() SourceInfo {
	return SourceInfo{
		Name:        "ebay",
		Description: "API eBay Browse (recherche d'articles par mots-cles)",
		Categories:  []string{"api"},
		Requires:    []string{"EBAY_CLIENT_ID", "EBAY_CLIENT_SECRET", "EBAY_SEARCH_QUERIES"},
		Version:     "1",
	}
}

func (s *Ebay) Fetch(ctx context.Context) ([]domain.Deal, error) {
	token, err := s.ensureToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("ebay oauth: %w", err)
	}
	var deals []domain.Deal
	for _, q := range s.queries {
		items, err := s.search(ctx, token, q)
		if err != nil {
			slog.Warn("ebay search", "query", q, "err", err)
			continue
		}
		deals = append(deals, items...)
	}
	slog.Debug("ebay", "deals", len(deals))
	return deals, nil
}

// ensureToken returns a valid OAuth2 access token, refreshing it if necessary.
func (s *Ebay) ensureToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.tokenExpiry.Add(-60*time.Second)) {
		return s.token, nil
	}
	creds := base64.StdEncoding.EncodeToString([]byte(s.clientID + ":" + s.secret))
	body := "grant_type=client_credentials&scope=" + url.QueryEscape("https://api.ebay.com/oauth/api_scope")
	resp, err := s.http.PostWithHeaders(ctx, s.oauthBase, body, map[string]string{
		"Content-Type":  "application/x-www-form-urlencoded",
		"Authorization": "Basic " + creds,
	})
	if err != nil {
		return "", err
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal([]byte(resp), &tok); err != nil {
		return "", fmt.Errorf("parse token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response: %s", resp)
	}
	s.token = tok.AccessToken
	exp := 7200
	if tok.ExpiresIn > 0 {
		exp = tok.ExpiresIn
	}
	s.tokenExpiry = time.Now().Add(time.Duration(exp) * time.Second)
	slog.Debug("ebay token acquired", "expires_in", exp)
	return s.token, nil
}

func (s *Ebay) search(ctx context.Context, token, query string) ([]domain.Deal, error) {
	u := fmt.Sprintf("%s/buy/browse/v1/item_summary/search?q=%s&limit=50&filter=buyingOptions:{FIXED_PRICE}",
		s.apiBase, url.QueryEscape(query))
	resp, err := s.http.GetWithAuth(ctx, u, "Bearer "+token)
	if err != nil {
		return nil, err
	}
	var result struct {
		ItemSummaries []struct {
			ItemID        string `json:"itemId"`
			Title         string `json:"title"`
			ItemWebURL    string `json:"itemWebUrl"`
			Price         struct {
				Value    string `json:"value"`
				Currency string `json:"currency"`
			} `json:"price"`
			Condition struct {
				ConditionID string `json:"conditionId"`
				DisplayName string `json:"condition"`
			} `json:"condition"`
			Brand string `json:"brand"`
			Image struct {
				ImageURL string `json:"imageUrl"`
			} `json:"image"`
		} `json:"itemSummaries"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return nil, fmt.Errorf("parse search: %w", err)
	}

	var deals []domain.Deal
	for _, it := range result.ItemSummaries {
		if it.Title == "" || it.ItemWebURL == "" || it.Price.Value == "" {
			continue
		}
		price, err := parseFloatClean(it.Price.Value)
		if err != nil || price <= 0 {
			continue
		}
		tb, err := parsing.ParseCapacityTB(it.Title)
		if err != nil || tb <= 0 {
			continue // not a disk listing with parseable capacity
		}
		pt := price / tb
		cond := conditionFromEbay(it.Condition.ConditionID, it.Condition.DisplayName)
		classText := it.Title + " " + it.Brand
		media := normalMedia(classText)
		dc := parsing.NormalizeDriveCategory(classText, media)
		ifaces := parsing.NormalizeInterfaces(classText)
		var extID *string
		if it.ItemID != "" {
			extID = &it.ItemID
		}
		deals = append(deals, domain.Deal{
			Source: "ebay", Title: it.Title, URL: it.ItemWebURL,
			PriceEUR: round2(price), PricePerTB: round2(pt), CapacityTB: round2(tb),
			Condition: &cond, MediaType: media,
			ExternalID:    extID,
			DriveCategory: dc, Interfaces: ifaces,
			Brand: strPtr(it.Brand),
			ObservedAt: domain.UTCNow(),
		})
	}
	return deals, nil
}

// conditionFromEbay maps eBay condition IDs to our Condition type.
// eBay uses numeric condition IDs: 1000=New, 2000-3000=Refurbished/Used.
func conditionFromEbay(condID, displayName string) domain.Condition {
	id := strings.TrimSpace(condID)
	if strings.HasPrefix(id, "1") { // 1000 = New
		return domain.ConditionNew
	}
	lname := strings.ToLower(displayName)
	if strings.Contains(lname, "new") {
		return domain.ConditionNew
	}
	return domain.ConditionUsed
}
