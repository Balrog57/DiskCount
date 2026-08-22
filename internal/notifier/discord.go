package notifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
)

type Discord struct {
	token, channelID, baseURL string
	client                    *http.Client
}

func NewDiscord(token, channelID string) *Discord {
	return &Discord{
		token: token, channelID: channelID,
		baseURL: "https://discord.com/api/v10",
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (d *Discord) SendDeal(alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision) error {
	if d == nil || d.token == "" || d.channelID == "" {
		return fmt.Errorf("discord non configure")
	}
	content := fmt.Sprintf("🔔 **%s**\n**%s**\n💰 **%.2f €** · %.2f €/To · %.1f To\n%s · %s",
		alert.Name, deal.Title, deal.PriceEUR, deal.PricePerTB, deal.CapacityTB, deal.Source, deal.URL)
	if dec.DiscountPct != nil && *dec.DiscountPct > 0 {
		content += fmt.Sprintf("\n📉 Baisse de %.1f %%", *dec.DiscountPct)
	}
	if runes := []rune(content); len(runes) > 2000 {
		content = string(runes[:1997]) + "..."
	}
	return d.sendContent(content)
}

// SendTest sends a fixed, non-mentioning message to verify the configured
// Discord destination. It deliberately contains no user-provided content.
func (d *Discord) SendTest() error {
	return d.sendContent("DiskCount : message de test Discord.")
}

func (d *Discord) sendContent(content string) error {
	if d == nil || d.token == "" || d.channelID == "" {
		return fmt.Errorf("discord non configure")
	}
	payload, _ := json.Marshal(map[string]any{
		"content":          content,
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	req, err := http.NewRequest(http.MethodPost, d.baseURL+"/channels/"+d.channelID+"/messages", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+d.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("discord %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}
