package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Balrog57/DiskCount/internal/config"
	"github.com/Balrog57/DiskCount/internal/db"
	"github.com/Balrog57/DiskCount/internal/rules"
	"github.com/Balrog57/DiskCount/internal/scanner"
	tele "gopkg.in/telebot.v3"
)

var wizardSteps = []string{"media", "condition", "capacity", "price", "categories", "interfaces", "confirm"}

type alertDraft struct {
	step            string
	name            string
	capacityPresets []string
	maxPricePerTB   *float64
	conditions      []string
	mediaTypes      []string
	driveCategories []string
	interfaces      []string
	sources         []string
	updatedAt       time.Time
}

type Bot struct {
	TB      *tele.Bot
	db      *db.DB
	cfg     *config.Config
	scanner *scanner.Scanner
	drafts  map[int64]*alertDraft
	pend    map[int64]string
	mu      sync.RWMutex
}

var priceLabels = []struct {
	Key   string
	Label string
	Value *float64
	Media string
}{
	{"none", "Aucune limite", nil, "all"},
	{"h15", "HDD <=15 EUR/To", pf(15), "rotational"},
	{"h18", "HDD <=18 EUR/To", pf(18), "rotational"},
	{"h20", "HDD <=20 EUR/To", pf(20), "rotational"},
	{"h22", "HDD <=22 EUR/To", pf(22), "rotational"},
	{"h25", "HDD <=25 EUR/To", pf(25), "rotational"},
	{"s004", "SSD <=0.04 EUR/Go", pf(40), "solid_state"},
	{"s006", "SSD <=0.06 EUR/Go", pf(60), "solid_state"},
	{"s008", "SSD <=0.08 EUR/Go", pf(80), "solid_state"},
	{"s010", "SSD <=0.10 EUR/Go", pf(100), "solid_state"},
	{"s012", "SSD <=0.12 EUR/Go", pf(120), "solid_state"},
}

var (
	hddCapKeys = []string{"hdd_lt_4", "hdd_4_8", "hdd_8_12", "hdd_12_16", "hdd_16_20", "hdd_20_24", "hdd_24_30", "hdd_gt_30"}
	ssdCapKeys = []string{"ssd_lt_256", "ssd_256", "ssd_512", "ssd_1", "ssd_2", "ssd_4", "ssd_gt_4"}
	hddPrKeys  = []string{"h15", "h18", "h20", "h22", "h25"}
	ssdPrKeys  = []string{"s004", "s006", "s008", "s010", "s012"}
)

var catPairs = [][2]string{
	{"External 3.5", "external_3_5"}, {"External 2.5", "external_2_5"},
	{"Internal 3.5", "internal_3_5"}, {"Internal 2.5", "internal_2_5"},
	{"Hybrid", "internal_hybrid"}, {"Internal SAS", "internal_sas"},
	{"External SSD", "external_ssd"}, {"Internal SSD", "internal_ssd"},
	{"M.2 SATA", "m2_sata"}, {"M.2 NVMe", "m2_nvme"}, {"U.2/U.3", "u2_u3"},
}
var ifacePairs = [][2]string{{"SATA", "sata"}, {"SAS", "sas"}, {"NVMe", "nvme"}, {"USB", "usb"}}

func pf(f float64) *float64 { return &f }

func New(cfg *config.Config, dbase *db.DB, scan *scanner.Scanner) (*Bot, error) {
	tb, err := tele.NewBot(tele.Settings{Token: cfg.TelegramBotToken, Poller: &tele.LongPoller{Timeout: 10 * time.Second}})
	if err != nil {
		return nil, err
	}
	b := &Bot{TB: tb, db: dbase, cfg: cfg, scanner: scan, drafts: make(map[int64]*alertDraft), pend: make(map[int64]string)}
	b.setup()
	return b, nil
}

func (b *Bot) setup() {
	b.TB.Handle("/start", b.auth(b.start))
	b.TB.Handle("/menu", b.auth(b.menu))
	b.TB.Handle("/help", b.auth(b.help))
	b.TB.Handle("/create", b.auth(b.createCmd))
	b.TB.Handle("/add", b.auth(b.addCmd))
	b.TB.Handle("/alerts", b.auth(b.alertsCmd))
	b.TB.Handle("/pause", b.auth(b.pauseCmd))
	b.TB.Handle("/resume", b.auth(b.resumeCmd))
	b.TB.Handle("/delete", b.auth(b.deleteCmd))
	b.TB.Handle("/set_max_price", b.auth(b.setMaxPriceCmd))
	b.TB.Handle(tele.OnCallback, b.callback)
	b.TB.Handle(tele.OnText, b.onText)

	cmds := []tele.Command{
		{Text: "start", Description: "Demarrer le bot"}, {Text: "menu", Description: "Navigation par tuiles"},
		{Text: "create", Description: "Creer une alerte (tuiles)"}, {Text: "help", Description: "Aide et filtres"},
		{Text: "add", Description: "Ajouter une alerte (texte)"}, {Text: "alerts", Description: "Lister tes alertes"},
		{Text: "pause", Description: "Mettre en pause"}, {Text: "resume", Description: "Reactiver"},
		{Text: "delete", Description: "Supprimer"}, {Text: "set_max_price", Description: "Modifier seuil EUR/To"},
	}
	b.TB.SetCommands(cmds)
	ac := append(cmds, tele.Command{Text: "users", Description: "Utilisateurs autorises"},
		tele.Command{Text: "allow", Description: "Autoriser un utilisateur"},
		tele.Command{Text: "revoke", Description: "Retirer l'acces"})
	for _, id := range b.cfg.TelegramAdminUserIDs {
		b.TB.SetCommands(ac, tele.CommandScope{ChatID: id})
	}
}

func (b *Bot) auth(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if !b.allowed(c.Sender().ID) {
			return c.Send("Acces refuse.")
		}
		return next(c)
	}
}

func (b *Bot) allowed(uid int64) bool {
	for _, id := range b.cfg.TelegramAdminUserIDs {
		if uid == id {
			return true
		}
	}
	ok, _ := b.db.IsUserAllowed(context.Background(), uid)
	return ok
}
func (b *Bot) canAd(c tele.Context) bool {
	for _, id := range b.cfg.TelegramAdminUserIDs {
		if c.Sender().ID == id {
			return true
		}
	}
	return false
}

func (b *Bot) draft(uid int64) *alertDraft {
	b.mu.Lock()
	defer b.mu.Unlock()
	d := b.drafts[uid]
	if d == nil || time.Since(d.updatedAt) > time.Hour {
		d = newDraft()
		b.drafts[uid] = d
	}
	d.updatedAt = time.Now()
	return d
}
func newDraft() *alertDraft {
	v := 20.0
	return &alertDraft{step: "media", name: "Alerte DiskCount", capacityPresets: []string{"hdd_16_20"}, conditions: []string{"new", "used"}, mediaTypes: []string{"rotational"}, maxPricePerTB: &v, updatedAt: time.Now()}
}

func nxtStep(s string) string {
	for i, v := range wizardSteps {
		if v == s {
			return wizardSteps[min(i+1, len(wizardSteps)-1)]
		}
	}
	return s
}
func prvStep(s string) string {
	for i, v := range wizardSteps {
		if v == s {
			return wizardSteps[max(i-1, 0)]
		}
	}
	return s
}
func tglV(l []string, v string) []string {
	for i, x := range l {
		if x == v {
			return append(l[:i], l[i+1:]...)
		}
	}
	return append(l, v)
}
func hV(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

var mHome = "DiskCount\n\nChoisis une action.\n\nCreer une alerte lance le wizard complet. Mes alertes ouvre tes alertes pour les modifier, les pauser ou les supprimer. Scanner/Test verifie le bot sans envoyer de notification."

var helpTexts = map[string]string{
	"help:create":     "Guide - Creer une alerte\n\nLe wizard te fait choisir dans l'ordre: type de disque, etat, capacites, prix, categories, connexions, puis recapitulatif.\n\nTu peux cocher plusieurs capacites. Toute capacite vide la selection.",
	"help:alerts":     "Guide - Mes alertes\n\nChaque alerte apparait comme une tuile avec son nom, son etat, son type, sa capacite et son prix. Ouvre une tuile pour modifier l'alerte, la pauser, la reprendre ou la supprimer avec confirmation.",
	"help:capacity":   "Guide - Capacites\n\nLes plages sont multi-selection.\n\nSSD: <256 Go, ~256 Go, ~512 Go, ~1 To, ~2 To, ~4 To, >4 To.\nHDD: <4 To, 4-8 To, 8-12 To, 12-16 To, 16-20 To, 20-24 To, 24-30 To, >30 To.",
	"help:price":      "Guide - Prix\n\nHDD: seuils en EUR/To: 15, 18, 20, 22, 25 ou aucune limite.\nSSD: seuils en EUR/Go: 0.04, 0.06, 0.08, 0.10, 0.12 ou aucune limite.",
	"help:categories": "Guide - Categories\n\nHDD: External 3.5, External 2.5, Internal 3.5, Internal 2.5, Internal Hybrid, Internal SAS.\nSSD: External SSD, Internal SSD, M.2 SATA, M.2 NVMe, U.2/U.3.",
	"help:interfaces": "Guide - Connexions\n\nChoix disponibles: SATA, SAS, NVMe, USB.",
	"help:scan":       "Guide - Scanner/Test\n\nStatut affiche les compteurs, les sources chargees et l'intervalle de scan.\nTest lance un dry-run: collecte et matching sans envoyer de notification.",
	"help:admin":      "Guide - Admin\n\nVisible uniquement pour les admins. Permet d'ajouter, revoquer ou reactiver des utilisateurs.",
	"help:commands":   "/menu ouvre les tuiles.\n/create lance le wizard d'alerte.\n/alerts liste tes alertes.\n/add cree une alerte par texte.\n/pause, /resume, /delete gerent une alerte par ID.\n/set_max_price modifie le seuil.",
	"help:filters":    "name, min_tb, max_tb, max_eur_tb, max_eur_gb, condition, media, category, interface, sources, discount, cooldown",
	"admin":           "Admin\n\nGestion des utilisateurs autorises a utiliser le bot.",
}

func aStr(a *db.Alert) string {
	st := "on"
	if !a.Enabled {
		st = "off"
	}
	c := fmCap(a)
	p := ""
	if a.MaxPricePerTB != nil {
		p = fmt.Sprintf("prix<=%.0fEUR/To", *a.MaxPricePerTB)
	}
	return fmt.Sprintf("#%d [%s] %s | capacite=%s | %s | remise>=%.0f%%", a.ID, st, a.Name, c, p, a.MinDiscountPct)
}
func fmCap(a *db.Alert) string {
	if len(a.CapacityPresets) > 0 {
		var ns []string
		for _, k := range a.CapacityPresets {
			if p, ok := rules.CapacityPresets[k]; ok {
				ns = append(ns, p.Label)
			}
		}
		if len(ns) > 0 {
			return strings.Join(ns, ", ")
		}
	}
	return "toute capacite"
}

func fDraft(d *alertDraft) string {
	t := map[string]string{"media": "1/8 Type de disque", "condition": "2/8 Etat produit", "capacity": "3/8 Capacite", "price": "4/8 Prix", "categories": "5/8 Categories", "interfaces": "6/8 Connexions", "confirm": "7/7 Recapitulatif"}
	h := map[string]string{"media": "Choisis HDD, SSD, ou les deux.", "condition": "Choisis New, Used, ou les deux.", "capacity": "Selectionne des plages.", "price": "Choisis un prix max.", "categories": "Filtre les familles.", "interfaces": "Filtre les connexions.", "confirm": "Verifie puis cree."}
	return fmt.Sprintf("%s\n\n%s\n\n%s", t[d.step], fDS(d), h[d.step])
}
func fDS(d *alertDraft) string {
	c := "toute capacite"
	if len(d.capacityPresets) > 0 {
		var ns []string
		for _, k := range d.capacityPresets {
			if p, ok := rules.CapacityPresets[k]; ok {
				ns = append(ns, p.Label)
			}
		}
		if len(ns) > 0 {
			c = strings.Join(ns, ", ")
		}
	}
	pr := "aucune limite"
	if d.maxPricePerTB != nil {
		pr = fmt.Sprintf("%.0f EUR/To", *d.maxPricePerTB)
	}
	return fmt.Sprintf("Nom: %s\nType: %s\nEtat: %s\nCapacite: %s\nPrix max: %s\nCategories: %s\nConnexions: %s", d.name, fv(d.mediaTypes), fv(d.conditions), c, pr, fv(d.driveCategories), fv(d.interfaces))
}
func fv(v []string) string {
	if len(v) == 0 {
		return "tous"
	}
	var ls []string
	for _, x := range v {
		ls = append(ls, dv(x))
	}
	return strings.Join(ls, ", ")
}
func dv(v string) string {
	m := map[string]string{"rotational": "HDD", "solid_state": "SSD", "new": "New", "used": "Used", "external_3_5": "External 3.5", "external_2_5": "External 2.5", "internal_3_5": "Internal 3.5", "internal_2_5": "Internal 2.5", "internal_hybrid": "Hybrid", "internal_sas": "Internal SAS", "external_ssd": "External SSD", "internal_ssd": "Internal SSD", "m2_sata": "M.2 SATA", "m2_nvme": "M.2 NVMe", "u2_u3": "U.2/U.3", "sata": "SATA", "sas": "SAS", "nvme": "NVMe", "usb": "USB"}
	if l, ok := m[v]; ok {
		return l
	}
	return v
}

func ib(data, text string) tele.InlineButton {
	return tele.InlineButton{Text: text, Data: data, Unique: data}
}

func homeKB(ad bool) *tele.ReplyMarkup {
	return kb(ib("draft:start", "Creer une alerte"), ib("menu:alerts:list", "Mes alertes"), ib("menu:scan", "Scanner/Test"), ib("menu:help", "Aide"), ad)
}
func kb(buttons ...interface{}) *tele.ReplyMarkup {
	var btns []tele.InlineButton
	for _, b := range buttons {
		switch v := b.(type) {
		case tele.InlineButton:
			btns = append(btns, v)
		}
	}
	var r [][]tele.InlineButton
	if len(btns) >= 2 {
		r = [][]tele.InlineButton{btns[:2]}
	}
	if len(btns) > 2 {
		r = append(r, btns[2:])
	}
	return &tele.ReplyMarkup{InlineKeyboard: r}
}

func menuKB(v string, ad bool) *tele.ReplyMarkup {
	nav := []tele.InlineButton{ib("menu:"+mp(v), "Precedent"), ib("menu:home", "Accueil")}
	switch {
	case v == "home":
		return homeKB(ad)
	case v == "scan":
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("menu:scan:status", "Statut"), ib("menu:scan:test", "Test")}, nav}}
	case strings.HasPrefix(v, "help"):
		rows := [][]tele.InlineButton{
			{ib("menu:help:create", "Creer"), ib("menu:help:alerts", "Alertes")},
			{ib("menu:help:capacity", "Capacites"), ib("menu:help:price", "Prix")},
			{ib("menu:help:categories", "Categories"), ib("menu:help:interfaces", "Connexions")},
			{ib("menu:help:scan", "Scanner"), ib("menu:help:admin", "Admin")},
			{ib("menu:help:commands", "Commandes"), ib("menu:help:filters", "Filtres")},
			nav,
		}
		return &tele.ReplyMarkup{InlineKeyboard: rows}
	case v == "admin":
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("admin:list", "Liste"), ib("admin:add", "Ajouter")}, {ib("admin:revoke", "Revoquer")}, nav}}
	default:
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{nav}}
	}
}
func mp(v string) string {
	if !strings.Contains(v, ":") {
		return "home"
	}
	return v[:strings.LastIndex(v, ":")]
}

func alertsKB(as []db.Alert, ad bool) *tele.ReplyMarkup {
	var r [][]tele.InlineButton
	for _, a := range as {
		r = append(r, []tele.InlineButton{ib("alert:edit:"+is(a.ID), fAb(&a))})
	}
	r = append(r, []tele.InlineButton{ib("draft:start", "Creer une alerte")})
	r = append(r, []tele.InlineButton{ib("menu:alerts:list", "Precedent"), ib("menu:home", "Accueil")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func fAb(a *db.Alert) string {
	st := "on"
	if !a.Enabled {
		st = "off"
	}
	m := "HDD/SSD"
	if len(a.MediaTypes) > 0 {
		m = strings.Join(a.MediaTypes, ",")
	}
	pr := "prix libre"
	if a.MaxPricePerTB != nil {
		pr = fmt.Sprintf("prix<=%.0fEUR/To", *a.MaxPricePerTB)
	}
	return fmt.Sprintf("#%d %s | %s | %s | %s | %s", a.ID, a.Name, st, m, fmCap(a), pr)
}
func editKB(a *db.Alert) *tele.ReplyMarkup {
	sl := "Pauser"
	if !a.Enabled {
		sl = "Reprendre"
	}
	s := is(a.ID)
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
		{ib("alert:enabled:"+s, sl)},
		{ib("alert:media:"+s, "Type"), ib("alert:condition:"+s, "Etat")},
		{ib("alert:capacity:"+s, "Capacite"), ib("alert:price:"+s, "Prix")},
		{ib("alert:categories:"+s, "Categories"), ib("alert:interfaces:"+s, "Connexions")},
		{ib("alert:delete:"+s, "Supprimer")},
		{ib("menu:alerts:list", "Precedent"), ib("menu:home", "Accueil")},
	}}
}
func is(id int64) string { return strconv.FormatInt(id, 10) }

func lTgl(l, v string, sel []string) string {
	if hV(sel, v) {
		return "[x] " + l
	}
	return "[ ] " + l
}
func lPres(k string, sel []string) string {
	p, ok := rules.CapacityPresets[k]
	if !ok {
		return "[ ] ??"
	}
	if k == "all" && len(sel) == 0 {
		return "[x] " + p.Label
	}
	if hV(sel, k) {
		return "[x] " + p.Label
	}
	return "[ ] " + p.Label
}

func alTglKB(f string, id int64, pairs [][2]string, sel []string) *tele.ReplyMarkup {
	var r [][]tele.InlineButton
	var c []tele.InlineButton
	for _, p := range pairs {
		c = append(c, ib(fmt.Sprintf("alert:toggle:%d:%s:%s", id, f, p[1]), lTgl(p[0], p[1], sel)))
		if len(c) == 2 {
			r = append(r, c)
			c = nil
		}
	}
	if len(c) > 0 {
		r = append(r, c)
	}
	r = append(r, []tele.InlineButton{ib(fmt.Sprintf("alert:edit:%d", id), "Precedent"), ib("menu:home", "Accueil")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}

func alCapKB(a *db.Alert) *tele.ReplyMarkup {
	s := is(a.ID)
	r := [][]tele.InlineButton{{ib("alert:cap:"+s+":all", lPres("all", a.CapacityPresets))}}
	r = append(r, prRows("alert:cap:"+s, hddCapKeys, a.CapacityPresets)...)
	r = append(r, prRows("alert:cap:"+s, ssdCapKeys, a.CapacityPresets)...)
	r = append(r, []tele.InlineButton{ib("alert:edit:"+s, "Precedent"), ib("menu:home", "Accueil")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func prRows(p string, ks []string, sel []string) [][]tele.InlineButton {
	var r [][]tele.InlineButton
	var c []tele.InlineButton
	for _, k := range ks {
		c = append(c, ib(p+":"+k, lPres(k, sel)))
		if len(c) == 2 {
			r = append(r, c)
			c = nil
		}
	}
	if len(c) > 0 {
		r = append(r, c)
	}
	return r
}

func alPriceKB(a *db.Alert) *tele.ReplyMarkup {
	s := is(a.ID)
	kk := prKey(a)
	r := [][]tele.InlineButton{{ib("alert:price_set:"+s+":none", "Aucune limite")}}
	pr := func(ks []string) {
		var c []tele.InlineButton
		for _, k := range ks {
			for _, pl := range priceLabels {
				if pl.Key == k {
					l := pl.Label
					if k == kk {
						l = "[x] " + l
					}
					c = append(c, ib("alert:price_set:"+s+":"+k, l))
					break
				}
			}
			if len(c) == 2 {
				r = append(r, c)
				c = nil
			}
		}
		if len(c) > 0 {
			r = append(r, c)
		}
	}
	pr(hddPrKeys)
	pr(ssdPrKeys)
	r = append(r, []tele.InlineButton{ib("alert:edit:"+s, "Precedent"), ib("menu:home", "Accueil")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func prKey(a *db.Alert) string {
	if a.MaxPricePerTB == nil {
		return "none"
	}
	for _, pl := range priceLabels {
		if pl.Value != nil && fEq(*pl.Value, *a.MaxPricePerTB) {
			return pl.Key
		}
	}
	return ""
}
func fEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.01
}

func draftKB(d *alertDraft, ad bool) *tele.ReplyMarkup {
	nav := []tele.InlineButton{ib("draft:prev", "Precedent"), ib("draft:next", "Suivant"), ib("menu:home", "Accueil")}
	switch d.step {
	case "media":
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("draft:toggle:media:rotational", lTgl("HDD", "rotational", d.mediaTypes)), ib("draft:toggle:media:solid_state", lTgl("SSD", "solid_state", d.mediaTypes))}, nav}}
	case "condition":
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("draft:toggle:condition:new", lTgl("New", "new", d.conditions)), ib("draft:toggle:condition:used", lTgl("Used", "used", d.conditions))}, nav}}
	case "capacity":
		r := [][]tele.InlineButton{{ib("draft:cap:all", lPres("all", d.capacityPresets))}}
		r = append(r, prRows("draft:cap", hddCapKeys, d.capacityPresets)...)
		r = append(r, prRows("draft:cap", ssdCapKeys, d.capacityPresets)...)
		r = append(r, nav)
		return &tele.ReplyMarkup{InlineKeyboard: r}
	case "price":
		r := [][]tele.InlineButton{{ib("draft:price:none", "Aucune limite")}}
		kk := ""
		if d.maxPricePerTB != nil {
			for _, pl := range priceLabels {
				if pl.Value != nil && fEq(*pl.Value, *d.maxPricePerTB) {
					kk = pl.Key
					break
				}
			}
		}
		pr := func(ks []string) {
			var c []tele.InlineButton
			for _, k := range ks {
				for _, pl := range priceLabels {
					if pl.Key == k {
						l := pl.Label
						if k == kk {
							l = "[x] " + l
						}
						c = append(c, ib("draft:price:"+k, l))
						break
					}
				}
				if len(c) == 2 {
					r = append(r, c)
					c = nil
				}
			}
			if len(c) > 0 {
				r = append(r, c)
			}
		}
		pr(hddPrKeys)
		pr(ssdPrKeys)
		r = append(r, nav)
		return &tele.ReplyMarkup{InlineKeyboard: r}
	case "categories":
		r := optRows(catPairs, d.driveCategories, "draft:toggle:category")
		r = append(r, nav)
		return &tele.ReplyMarkup{InlineKeyboard: r}
	case "interfaces":
		r := optRows(ifacePairs, d.interfaces, "draft:toggle:interface")
		r = append(r, nav)
		return &tele.ReplyMarkup{InlineKeyboard: r}
	default:
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("draft:create", "Creer")}, {ib("draft:prev", "Precedent"), ib("draft:cancel", "Annuler")}, {ib("menu:home", "Accueil")}}}
	}
}
func optRows(ps [][2]string, sel []string, px string) [][]tele.InlineButton {
	var r [][]tele.InlineButton
	var c []tele.InlineButton
	for _, p := range ps {
		c = append(c, ib(px+":"+p[1], lTgl(p[0], p[1], sel)))
		if len(c) == 2 {
			r = append(r, c)
			c = nil
		}
	}
	if len(c) > 0 {
		r = append(r, c)
	}
	return r
}

// Handlers
func (b *Bot) start(c tele.Context) error {
	b.db.UpsertSubscriber(context.Background(), c.Chat().ID, nil)
	return c.Send(mHome, &tele.SendOptions{ReplyMarkup: homeKB(b.canAd(c))})
}
func (b *Bot) menu(c tele.Context) error {
	return c.Send(mHome, &tele.SendOptions{ReplyMarkup: homeKB(b.canAd(c))})
}
func (b *Bot) help(c tele.Context) error {
	return c.Send("Aide\n\nChoisis un theme.", &tele.SendOptions{ReplyMarkup: menuKB("help", b.canAd(c))})
}

func (b *Bot) alertsCmd(c tele.Context) error {
	as, _ := b.db.GetAlertsByOwner(context.Background(), c.Sender().ID, false)
	if len(as) == 0 {
		return c.Send("Aucune alerte. Utilise /create.", &tele.SendOptions{ReplyMarkup: menuKB("home", b.canAd(c))})
	}
	var ls []string
	for _, a := range as {
		ls = append(ls, aStr(&a))
	}
	return c.Send("Mes alertes\n\n"+strings.Join(ls, "\n"), &tele.SendOptions{ReplyMarkup: alertsKB(as, b.canAd(c))})
}

func (b *Bot) addCmd(c tele.Context) error {
	p := c.Message().Payload
	if p == "" {
		return c.Send("Usage: /add name=NAS max_eur_tb=20 capacity=hdd_16_20 condition=new media=rotational")
	}
	m := parseKV(p)
	name := m["name"]
	if name == "" {
		name = "Alerte DiskCount"
	}
	var mx *float64
	if v, e := strconv.ParseFloat(m["max_eur_tb"], 64); e == nil {
		mx = &v
	}
	caps := flds(m["capacity"])
	conds := flds(m["condition"])
	meds := flds(m["media"])
	a, err := b.db.CreateAlert(context.Background(), c.Chat().ID, c.Sender().ID, name, mx, 5.0, 24, caps, conds, meds, nil, nil, nil)
	if err != nil {
		return c.Send("Erreur: " + err.Error())
	}
	return c.Send(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
}

func (b *Bot) pauseCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 {
		return c.Send("Usage: /pause ID")
	}
	b.db.SetAlertEnabled(context.Background(), c.Sender().ID, id, false)
	return c.Send(fmt.Sprintf("Alerte #%d en pause.", id))
}
func (b *Bot) resumeCmd(c tele.Context) error { return c.Send("Usage: /resume ID") }
func (b *Bot) deleteCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 {
		return c.Send("Usage: /delete ID")
	}
	b.db.DeleteAlert(context.Background(), c.Sender().ID, id)
	return c.Send(fmt.Sprintf("Alerte #%d supprimee.", id))
}
func (b *Bot) setMaxPriceCmd(c tele.Context) error {
	f := strings.Fields(c.Message().Payload)
	if len(f) < 2 {
		return c.Send("Usage: /set_max_price ID 20 ou ID none")
	}
	id, _ := strconv.ParseInt(f[0], 10, 64)
	var p *float64
	if f[1] != "none" {
		v, _ := strconv.ParseFloat(f[1], 64)
		p = &v
	}
	b.db.SetAlertMaxPrice(context.Background(), c.Sender().ID, id, p)
	return c.Send("Prix mis a jour.")
}
func (b *Bot) createCmd(c tele.Context) error {
	uid := c.Sender().ID
	b.mu.Lock()
	b.drafts[uid] = newDraft()
	b.mu.Unlock()
	d := b.draft(uid)
	return c.Send(fDraft(d), &tele.SendOptions{ReplyMarkup: draftKB(d, b.canAd(c))})
}

func (b *Bot) callback(c tele.Context) error {
	data := c.Callback().Data
	if data == "" || !b.allowed(c.Sender().ID) {
		c.Respond()
		return nil
	}
	ad := b.canAd(c)
	c.Respond()

	switch {
	case data == "menu:home":
		return c.Edit(mHome, &tele.SendOptions{ReplyMarkup: homeKB(ad)})
	case strings.HasPrefix(data, "menu:"):
		v := strings.TrimPrefix(data, "menu:")
		if v == "alerts:list" {
			as, _ := b.db.GetAlertsByOwner(context.Background(), c.Sender().ID, false)
			if len(as) == 0 {
				return c.Edit("Aucune alerte.", &tele.SendOptions{ReplyMarkup: menuKB("home", ad)})
			}
			var ls []string
			for _, a := range as {
				ls = append(ls, aStr(&a))
			}
			return c.Edit("Mes alertes\n\n"+strings.Join(ls, "\n"), &tele.SendOptions{ReplyMarkup: alertsKB(as, ad)})
		}
		if v == "scan:test" {
			r := b.scanner.RunOnce(context.Background(), true)
			return c.Edit(fmt.Sprintf("Dry-run\nOffres: %d | Matchs: %d | Alertes: %d\nErreurs: %d", r.Fetched, r.Matched, r.DryRunNotified, len(r.Errors)), &tele.SendOptions{ReplyMarkup: menuKB("scan:test", ad)})
		}
		if v == "scan:status" {
			var ns []string
			for _, s := range b.scanner.Sources() {
				ns = append(ns, s.Name())
			}
			return c.Edit(fmt.Sprintf("Status\nSources: %s", strings.Join(ns, ", ")), &tele.SendOptions{ReplyMarkup: menuKB("scan:status", ad)})
		}
		if strings.HasPrefix(v, "help") {
			txt := helpTexts[v]
			if txt == "" {
				txt = v
			}
			return c.Edit(txt, &tele.SendOptions{ReplyMarkup: menuKB(v, ad)})
		}
		return c.Edit("", &tele.SendOptions{ReplyMarkup: menuKB(v, ad)})

	case strings.HasPrefix(data, "draft:"):
		return b.draftCB(c, data, ad)
	case strings.HasPrefix(data, "alert:"):
		return b.alertCB(c, data, ad)
	case strings.HasPrefix(data, "admin:"):
		return b.adminCB(c, data, ad)
	}
	return nil
}

func (b *Bot) onText(c tele.Context) error {
	uid := c.Sender().ID
	b.mu.RLock()
	act, ok := b.pend[uid]
	b.mu.RUnlock()
	if !ok || act != "allow" {
		return nil
	}
	ps := strings.SplitN(c.Text(), " ", 2)
	if len(ps) < 2 {
		return c.Send("Format: ID Nom")
	}
	id, _ := strconv.ParseInt(ps[0], 10, 64)
	b.db.CreateAlert(context.Background(), c.Chat().ID, id, "Autorise "+ps[1], nil, 5.0, 24, nil, nil, nil, nil, nil, nil)
	b.mu.Lock()
	delete(b.pend, uid)
	b.mu.Unlock()
	return c.Send("Utilisateur autorise.", &tele.SendOptions{ReplyMarkup: menuKB("admin", true)})
}

func (b *Bot) draftCB(c tele.Context, d string, ad bool) error {
	uid := c.Sender().ID
	switch {
	case d == "draft:start":
		b.mu.Lock()
		b.drafts[uid] = newDraft()
		b.mu.Unlock()
		dr := b.draft(uid)
		return c.Edit(fDraft(dr), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
	case d == "draft:cancel":
		b.mu.Lock()
		delete(b.drafts, uid)
		b.mu.Unlock()
		return c.Edit("Creation annulee.", &tele.SendOptions{ReplyMarkup: homeKB(ad)})
	case d == "draft:next":
		dr := b.draft(uid)
		dr.step = nxtStep(dr.step)
		return c.Edit(fDraft(dr), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
	case d == "draft:prev":
		dr := b.draft(uid)
		dr.step = prvStep(dr.step)
		return c.Edit(fDraft(dr), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
	case strings.HasPrefix(d, "draft:toggle:"):
		ps := strings.Split(d, ":")
		dr := b.draft(uid)
		if len(ps) < 4 {
			break
		}
		switch ps[2] {
		case "media":
			dr.mediaTypes = tglV(dr.mediaTypes, ps[3])
		case "condition":
			dr.conditions = tglV(dr.conditions, ps[3])
		case "category":
			dr.driveCategories = tglV(dr.driveCategories, ps[3])
		case "interface":
			dr.interfaces = tglV(dr.interfaces, ps[3])
		}
		return c.Edit(fDraft(dr), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
	case strings.HasPrefix(d, "draft:cap:"):
		k := d[strings.LastIndex(d, ":")+1:]
		dr := b.draft(uid)
		if p, ok := rules.CapacityPresets[k]; ok {
			if k == "all" {
				dr.capacityPresets = nil
			} else if hV(dr.capacityPresets, k) {
				dr.capacityPresets = tglV(dr.capacityPresets, k)
			} else {
				dr.capacityPresets = append(dr.capacityPresets, k)
			}
			if k != "all" && p.MediaType != "all" && !hV(dr.mediaTypes, p.MediaType) {
				dr.mediaTypes = append(dr.mediaTypes, p.MediaType)
			}
		}
		return c.Edit(fDraft(dr), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
	case strings.HasPrefix(d, "draft:price:"):
		k := d[strings.LastIndex(d, ":")+1:]
		dr := b.draft(uid)
		for _, pl := range priceLabels {
			if pl.Key == k {
				dr.maxPricePerTB = pl.Value
				if pl.Media != "all" && !hV(dr.mediaTypes, pl.Media) {
					dr.mediaTypes = append(dr.mediaTypes, pl.Media)
				}
				break
			}
		}
		return c.Edit(fDraft(dr), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
	case d == "draft:create":
		dr := b.draft(uid)
		nm := dr.name
		if nm == "" {
			nm = "Alerte DiskCount"
		}
		a, err := b.db.CreateAlert(context.Background(), c.Chat().ID, uid, nm, dr.maxPricePerTB, 5.0, 24, dr.capacityPresets, dr.conditions, dr.mediaTypes, dr.driveCategories, dr.interfaces, dr.sources)
		if err != nil {
			return c.Edit("Erreur: "+err.Error(), &tele.SendOptions{ReplyMarkup: draftKB(dr, ad)})
		}
		b.mu.Lock()
		delete(b.drafts, uid)
		b.mu.Unlock()
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
	}
	return nil
}

func (b *Bot) alertCB(c tele.Context, d string, ad bool) error {
	ps := strings.Split(d, ":")
	if len(ps) < 2 {
		return nil
	}
	act := ps[1]
	aID, _ := strconv.ParseInt(ps[2], 10, 64)
	uid := c.Sender().ID
	a, _ := b.db.GetAlert(context.Background(), uid, aID)
	if a == nil {
		return c.Send("Alerte introuvable.")
	}

	switch act {
	case "edit":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
	case "enabled":
		b.db.SetAlertEnabled(context.Background(), uid, aID, !a.Enabled)
		if a2, _ := b.db.GetAlert(context.Background(), uid, aID); a2 != nil {
			a = a2
		}
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
	case "delete":
		return c.Edit("Supprimer #"+ps[2], &tele.SendOptions{ReplyMarkup: &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
			{ib("alert:delete_confirm:"+ps[2], "Confirmer suppression #"+ps[2])},
			{ib("alert:edit:"+ps[2], "Precedent"), ib("menu:home", "Accueil")},
		}}})
	case "delete_confirm":
		b.db.DeleteAlert(context.Background(), uid, aID)
		as, _ := b.db.GetAlertsByOwner(context.Background(), uid, false)
		return c.Edit("Supprimee.", &tele.SendOptions{ReplyMarkup: alertsKB(as, ad)})
	case "media":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("media", aID, [][2]string{{"HDD", "rotational"}, {"SSD", "solid_state"}}, a.MediaTypes)})
	case "condition":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("condition", aID, [][2]string{{"New", "new"}, {"Used", "used"}}, a.Conditions)})
	case "capacity":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alCapKB(a)})
	case "price":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alPriceKB(a)})
	case "categories":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("category", aID, catPairs, a.DriveCategories)})
	case "interfaces":
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("interface", aID, ifacePairs, a.Interfaces)})
	case "toggle":
		if len(ps) >= 5 {
			b.db.ToggleAlertFilter(context.Background(), uid, aID, ps[3], ps[4])
			if a2, _ := b.db.GetAlert(context.Background(), uid, aID); a2 != nil {
				a = a2
			}
			switch ps[3] {
			case "media":
				return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("media", aID, [][2]string{{"HDD", "rotational"}, {"SSD", "solid_state"}}, a.MediaTypes)})
			case "condition":
				return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("condition", aID, [][2]string{{"New", "new"}, {"Used", "used"}}, a.Conditions)})
			case "category":
				return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("category", aID, catPairs, a.DriveCategories)})
			case "interface":
				return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: alTglKB("interface", aID, ifacePairs, a.Interfaces)})
			}
		}
	case "cap":
		if len(ps) >= 4 {
			k := ps[3]
			if _, ok := rules.CapacityPresets[k]; ok {
				var nps []string
				if k == "all" {
					nps = nil
				} else if hV(a.CapacityPresets, k) {
					nps = tglV(a.CapacityPresets, k)
				} else {
					nps = append(a.CapacityPresets, k)
				}
				b.db.UpdateAlertCaps(context.Background(), uid, aID, nps)
				a.CapacityPresets = nps
			}
		}
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
	case "price_set":
		if len(ps) >= 4 {
			k := ps[3]
			for _, pl := range priceLabels {
				if pl.Key == k {
					b.db.SetAlertMaxPrice(context.Background(), uid, aID, pl.Value)
					if pl.Value != nil {
						a.MaxPricePerTB = pl.Value
					} else {
						a.MaxPricePerTB = nil
					}
					if pl.Media != "all" {
						b.db.ToggleAlertFilter(context.Background(), uid, aID, "media", pl.Media)
					}
					break
				}
			}
		}
		return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
	}
	return c.Edit(aStr(a), &tele.SendOptions{ReplyMarkup: editKB(a)})
}

func (b *Bot) adminCB(c tele.Context, d string, ad bool) error {
	switch d {
	case "admin:list":
		us, _ := b.db.ListAuthorizedUsers(context.Background(), true)
		var ls []string
		for _, u := range us {
			ls = append(ls, fmt.Sprintf("%s | %d", u.Label, u.TelegramUserID))
		}
		return c.Edit("Utilisateurs\n\n"+strings.Join(ls, "\n"), &tele.SendOptions{ReplyMarkup: menuKB("admin", true)})
	case "admin:add":
		b.mu.Lock()
		b.pend[c.Sender().ID] = "allow"
		b.mu.Unlock()
		return c.Edit("Ajouter\n\nEnvoie: USER_ID Label", &tele.SendOptions{ReplyMarkup: menuKB("admin", true)})
	case "admin:revoke":
		return c.Edit("Commande: /revoke ID", &tele.SendOptions{ReplyMarkup: menuKB("admin", true)})
	}
	return nil
}

func parseKV(t string) map[string]string {
	r := make(map[string]string)
	for _, p := range strings.Fields(t) {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			r[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return r
}
func flds(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

var _ = slog.Info
var _ = config.Config{}
var _ = db.Alert{}
var _ = rules.AlertMatches
var _ = scanner.Scanner{}
