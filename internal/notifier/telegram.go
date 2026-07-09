package notifier

import (
	"fmt"
	"github.com/Balrog57/DiskCount/internal/i18n"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/domain"
	tele "gopkg.in/telebot.v3"
	"strings"
	"time"
)

type TelegramNotifier struct {
	Bot       *tele.Bot
	DelaySecs float64
}

func New(bot *tele.Bot, d float64) *TelegramNotifier {
	return &TelegramNotifier{Bot: bot, DelaySecs: d}
}

func (n *TelegramNotifier) SendDeal(chatID int64, alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision) error {
	_, err := n.Bot.Send(tele.ChatID(chatID), fmtMsg(alert, deal, dec, i18n.FR), &tele.SendOptions{
		ParseMode: tele.ModeMarkdown,
		ReplyMarkup: &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
			{{Text: i18n.T("bot.open_offer", i18n.FR), URL: deal.URL}},
		}},
	})
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	if n.DelaySecs > 0 {
		time.Sleep(time.Duration(n.DelaySecs*1000) * time.Millisecond)
	}
	return nil
}

// SendAdminMessage posts an administrative message (no deal, no alert wiring)
// to a fixed chat ID. Used for source-health notifications and similar
// operator-facing pings. chatID must be > 0; otherwise the call is a no-op so
// unit tests can build a notifier without a configured admin.
func (n *TelegramNotifier) SendAdminMessage(chatID int64, text string) error {
	if n == nil || n.Bot == nil || chatID <= 0 {
		return nil
	}
	_, err := n.Bot.Send(tele.ChatID(chatID), text, &tele.SendOptions{
		ParseMode: tele.ModeMarkdown,
	})
	if err != nil {
		return fmt.Errorf("admin send: %w", err)
	}
	return nil
}

// FormatSourceHealthAlert renders the human message we send to the admin
// when a source has returned zero deals for too many consecutive scans.
// The locale is the admin's preferred language; the scanner passes the
// value it has in its config (FR by default).
func FormatSourceHealthAlert(name string, streak int, loc i18n.Locale) string {
	return fmt.Sprintf("⚠ *%s*: `%s` n'a retourne aucun deal depuis *%d* scans consecutifs. Verifier les selecteurs / le flux RSS.",
		i18n.T("notifier.source_health", loc), esc(name), streak)
}

func fmtMsg(alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision, loc i18n.Locale) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔥 *%s*\n\n", esc(alert.Name)))
	b.WriteString(fmt.Sprintf("*%s*\n", esc(deal.Title)))
	b.WriteString(fmt.Sprintf("💰 *%.2f€* \\(%.2f€/To\\)\n", deal.PriceEUR, deal.PricePerTB))
	b.WriteString(fmt.Sprintf("💾 %.1f To", deal.CapacityTB))
	if deal.Condition != nil {
		c := i18n.T("notifier.condition_new", loc)
		if *deal.Condition == "used" {
			c = i18n.T("notifier.condition_used", loc)
		}
		b.WriteString(fmt.Sprintf(" \\| %s", c))
	}
	if deal.MediaType != nil {
		m := i18n.T("notifier.media_hdd", loc)
		if *deal.MediaType == "solid_state" {
			m = i18n.T("notifier.media_ssd", loc)
		}
		b.WriteString(fmt.Sprintf(" \\| %s", m))
	}
	b.WriteString(fmt.Sprintf(" \\| %s\n", esc(deal.Source)))
	if dec.DiscountPct != nil && *dec.DiscountPct > 0 {
		b.WriteString(fmt.Sprintf("📉 *\\-%.1f%%* ", *dec.DiscountPct))
	}
	if dec.BaselinePricePerTB != nil {
		b.WriteString(fmt.Sprintf("\\(baseline: %.2f€/To\\)\n", *dec.BaselinePricePerTB))
	}
	return b.String()
}

func esc(s string) string {
	r := strings.NewReplacer("_", "\\_", "*", "\\*", "[", "\\[", "]", "\\]", "(", "\\(", ")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>", "#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=", "|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!")
	return r.Replace(s)
}
