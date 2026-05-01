package notifier

import (
	"fmt"
	"strings"
	"time"
	"github.com/MarcPartensky/DiskCount/internal/db"
	"github.com/MarcPartensky/DiskCount/internal/domain"
	tele "gopkg.in/telebot.v3"
)

type TelegramNotifier struct {
	Bot       *tele.Bot
	DelaySecs float64
}

func New(bot *tele.Bot, d float64) *TelegramNotifier { return &TelegramNotifier{Bot: bot, DelaySecs: d} }

func (n *TelegramNotifier) SendDeal(chatID int64, alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision) error {
	_, err := n.Bot.Send(tele.ChatID(chatID), fmtMsg(alert, deal, dec), &tele.SendOptions{
		ParseMode: tele.ModeMarkdown,
		ReplyMarkup: &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
			{{Text: "Ouvrir l'offre", URL: deal.URL}},
		}},
	})
	if err != nil { return fmt.Errorf("send: %w", err) }
	if n.DelaySecs > 0 { time.Sleep(time.Duration(n.DelaySecs*1000) * time.Millisecond) }
	return nil
}

func fmtMsg(alert *db.Alert, deal domain.Deal, dec domain.NotificationDecision) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔥 *%s*\n\n", esc(alert.Name)))
	b.WriteString(fmt.Sprintf("*%s*\n", esc(deal.Title)))
	b.WriteString(fmt.Sprintf("💰 *%.2f€* \\(%.2f€/To\\)\n", deal.PriceEUR, deal.PricePerTB))
	b.WriteString(fmt.Sprintf("💾 %.1f To", deal.CapacityTB))
	if deal.Condition != nil { c := "Neuf"; if *deal.Condition == "used" { c = "Occasion" }; b.WriteString(fmt.Sprintf(" \\| %s", c)) }
	if deal.MediaType != nil { m := "HDD"; if *deal.MediaType == "solid_state" { m = "SSD" }; b.WriteString(fmt.Sprintf(" \\| %s", m)) }
	b.WriteString(fmt.Sprintf(" \\| %s\n", esc(deal.Source)))
	if dec.DiscountPct != nil && *dec.DiscountPct > 0 { b.WriteString(fmt.Sprintf("📉 *\\-%.1f%%* ", *dec.DiscountPct)) }
	if dec.BaselinePricePerTB != nil { b.WriteString(fmt.Sprintf("\\(baseline: %.2f€/To\\)\n", *dec.BaselinePricePerTB)) }
	return b.String()
}

func esc(s string) string {
	r := strings.NewReplacer("_","\\_","*","\\*","[","\\[","]","\\]","(","\\(",")","\\)","~","\\~","`","\\`",">","\\>","#","\\#","+","\\+","-","\\-","=","\\=","|","\\|","{","\\{","}","\\}",".","\\.","!","\\!")
	return r.Replace(s)
}
