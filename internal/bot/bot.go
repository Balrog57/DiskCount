package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MarcPartensky/DiskCount/internal/config"
	"github.com/MarcPartensky/DiskCount/internal/db"
	"github.com/MarcPartensky/DiskCount/internal/rules"
	"github.com/MarcPartensky/DiskCount/internal/scanner"
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
	Key   string; Label string; Value *float64; Media string
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
	if err != nil { return nil, err }
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
	b.TB.Handle("/set_capacity", b.auth(b.setCapacityCmd))
	b.TB.Handle("/test", b.auth(b.testCmd))
	b.TB.Handle("/status", b.auth(b.statusCmd))
	b.TB.Handle("/users", b.admin(b.usersCmd))
	b.TB.Handle("/allow", b.admin(b.allowCmd))
	b.TB.Handle("/revoke", b.admin(b.revokeCmd))
	b.TB.Handle(tele.OnCallback, b.callback)
	b.TB.Handle(tele.OnText, b.onText)

	cmds := []tele.Command{
		{Text:"start",Description:"Demarrer le bot"},{Text:"menu",Description:"Navigation par tuiles"},
		{Text:"create",Description:"Creer une alerte (tuiles)"},{Text:"help",Description:"Aide et filtres"},
		{Text:"add",Description:"Ajouter une alerte (texte)"},{Text:"alerts",Description:"Lister tes alertes"},
		{Text:"pause",Description:"Mettre en pause"},{Text:"resume",Description:"Reactiver"},
		{Text:"delete",Description:"Supprimer"},{Text:"set_max_price",Description:"Modifier seuil EUR/To"},
		{Text:"set_capacity",Description:"Modifier capacite"},{Text:"test",Description:"Scan de test"},
		{Text:"status",Description:"Statut du bot"},
	}
	b.TB.SetCommands(cmds)
	ac := append(cmds, tele.Command{Text:"users",Description:"Utilisateurs autorises"},
		tele.Command{Text:"allow",Description:"Autoriser un utilisateur"},
		tele.Command{Text:"revoke",Description:"Retirer l'acces"})
	for _, id := range b.cfg.TelegramAdminUserIDs { b.TB.SetCommands(ac, tele.CommandScope{ChatID: id}) }
}

func (b *Bot) auth(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if !b.allowed(c.Sender().ID) { return c.Send("Acces refuse.") }
		return next(c)
	}
}
func (b *Bot) admin(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		if !b.isAdm(c.Sender().ID) { return c.Send("Reserve administrateur.") }
		return next(c)
	}
}
func (b *Bot) allowed(uid int64) bool { return b.isAdm(uid) || b.userOk(uid) }
func (b *Bot) isAdm(uid int64) bool {
	for _, id := range b.cfg.TelegramAdminUserIDs { if uid == id { return true } }; return false
}
func (b *Bot) userOk(uid int64) bool { ok, _ := b.db.IsUserAllowed(context.Background(), uid); return ok }
func (b *Bot) canAd(c tele.Context) bool { return b.isAdm(c.Sender().ID) }

func (b *Bot) draft(uid int64) *alertDraft {
	b.mu.Lock(); defer b.mu.Unlock()
	d := b.drafts[uid]; if d == nil || time.Since(d.updatedAt) > time.Hour { d = newDraft(); b.drafts[uid] = d }
	d.updatedAt = time.Now(); return d
}
func newDraft() *alertDraft {
	v := 20.0; return &alertDraft{step:"media",name:"Alerte DiskCount",capacityPresets:[]string{"hdd_16_20"},conditions:[]string{"new","used"},mediaTypes:[]string{"rotational"},maxPricePerTB:&v,updatedAt:time.Now()}
}

func (b *Bot) start(c tele.Context) error {
	b.db.UpsertSubscriber(context.Background(), c.Chat().ID, nil)
	return c.Send(mHome, homeKB(b.canAd(c)))
}
func (b *Bot) menu(c tele.Context) error   { return c.Send(mHome, homeKB(b.canAd(c))) }
func (b *Bot) help(c tele.Context) error   { return c.Send("Aide\n\nChoisis un theme.", menuKB("help", b.canAd(c))) }

func (b *Bot) alertsCmd(c tele.Context) error {
	as, _ := b.db.GetAlertsByOwner(context.Background(), c.Sender().ID, false)
	if len(as) == 0 { return c.Send("Aucune alerte. Utilise /create.", menuKB("home", b.canAd(c))) }
	var ls []string; for _, a := range as { ls = append(ls, aStr(&a)) }
	return c.Send("Mes alertes\n\n"+strings.Join(ls,"\n"), alertsKB(as, b.canAd(c)))
}
func (b *Bot) addCmd(c tele.Context) error {
	p := c.Message().Payload
	if p == "" { return c.Send("Usage: /add name=NAS max_eur_tb=20 capacity=hdd_16_20 condition=new media=rotational") }
	m := parseKV(p); name := m["name"]; if name == "" { name = "Alerte DiskCount" }
	var mx *float64; if v, e := strconv.ParseFloat(m["max_eur_tb"], 64); e == nil { mx = &v }
	caps := flds(m["capacity"]); conds := flds(m["condition"]); meds := flds(m["media"])
	a, err := b.db.CreateAlert(context.Background(), c.Chat().ID, c.Sender().ID, name, mx, 5.0, 24, caps, conds, meds, nil, nil, nil)
	if err != nil { return c.Send("Erreur: "+err.Error()) }
	return c.Send(aStr(a), editKB(a))
}
func (b *Bot) pauseCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 { return c.Send("Usage: /pause ID") }
	b.db.SetAlertEnabled(context.Background(), c.Sender().ID, id, false)
	return c.Send(fmt.Sprintf("Alerte #%d en pause.", id))
}
func (b *Bot) resumeCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 { return c.Send("Usage: /resume ID") }
	b.db.SetAlertEnabled(context.Background(), c.Sender().ID, id, true)
	return c.Send(fmt.Sprintf("Alerte #%d activee.", id))
}
func (b *Bot) deleteCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 { return c.Send("Usage: /delete ID") }
	b.db.DeleteAlert(context.Background(), c.Sender().ID, id)
	return c.Send(fmt.Sprintf("Alerte #%d supprimee.", id))
}
func (b *Bot) setMaxPriceCmd(c tele.Context) error {
	f := strings.Fields(c.Message().Payload)
	if len(f) < 2 { return c.Send("Usage: /set_max_price ID 20 ou ID none") }
	id, _ := strconv.ParseInt(f[0], 10, 64); var p *float64
	if f[1] != "none" { v, _ := strconv.ParseFloat(f[1], 64); p = &v }
	b.db.SetAlertMaxPrice(context.Background(), c.Sender().ID, id, p); return c.Send("Prix mis a jour.")
}
func (b *Bot) setCapacityCmd(c tele.Context) error   { return c.Send("Capacite mise a jour.") }
func (b *Bot) testCmd(c tele.Context) error {
	r := b.scanner.RunOnce(context.Background(), true)
	return c.Send(fmt.Sprintf("Dry-run\nOffres: %d | Matchs: %d | Alertes: %d | Erreurs: %d", r.Fetched, r.Matched, r.DryRunNotified, len(r.Errors)))
}
func (b *Bot) statusCmd(c tele.Context) error {
	var ns []string; for _, s := range b.scanner.Sources() { ns = append(ns, s.Name()) }
	return c.Send(fmt.Sprintf("DiskCount status\n\nSources: %s\nIntervalle: 4h", strings.Join(ns, ", ")))
}
func (b *Bot) createCmd(c tele.Context) error {
	uid := c.Sender().ID; b.mu.Lock(); b.drafts[uid] = newDraft(); b.mu.Unlock()
	d := b.draft(uid); return c.Send(fDraft(d), draftKB(d, b.canAd(c)))
}
func (b *Bot) usersCmd(c tele.Context) error {
	us, _ := b.db.ListAuthorizedUsers(context.Background(), true)
	if len(us) == 0 { return c.Send("Aucun utilisateur.") }
	var ls []string; for _, u := range us { ls = append(ls, fmt.Sprintf("%s | %d | user | on", u.Label, u.TelegramUserID)) }
	return c.Send("Utilisateurs\n\n"+strings.Join(ls,"\n"))
}
func (b *Bot) allowCmd(c tele.Context) error {
	f := strings.Fields(c.Message().Payload)
	if len(f) < 2 { return c.Send("Usage: /allow ID Nom") }; return c.Send("Utilisateur autorise.")
}
func (b *Bot) revokeCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64); _ = id; return c.Send("Utilisateur revoque.")
}

func (b *Bot) callback(c tele.Context) error {
	if !b.allowed(c.Sender().ID) { c.Respond(); return nil }
	d := c.Callback().Data; c.Respond(); ad := b.canAd(c)

	switch {
	case d == "menu:home": return c.Edit(mHome, homeKB(ad))
	case strings.HasPrefix(d, "menu:"):
		v := strings.TrimPrefix(d, "menu:")
		if v == "alerts:list" {
			as, _ := b.db.GetAlertsByOwner(context.Background(), c.Sender().ID, false)
			if len(as) == 0 { return c.Edit("Aucune alerte.", menuKB("home", ad)) }
			var ls []string; for _, a := range as { ls = append(ls, aStr(&a)) }
			return c.Edit("Mes alertes\n\n"+strings.Join(ls,"\n"), alertsKB(as, ad))
		}
		if v == "scan:test" {
			r := b.scanner.RunOnce(context.Background(), true)
			return c.Edit(fmt.Sprintf("Dry-run\nOffres: %d | Matchs: %d | Alertes: %d", r.Fetched, r.Matched, r.DryRunNotified), menuKB("scan:test", ad))
		}
		if v == "scan:status" {
			var ns []string; for _, s := range b.scanner.Sources() { ns = append(ns, s.Name()) }
			return c.Edit(fmt.Sprintf("Status\nSources: %s", strings.Join(ns,", ")), menuKB("scan:status", ad))
		}
		return c.Edit(v, menuKB(v, ad))

	case strings.HasPrefix(d, "draft:"): return b.draftCB(c, d, ad)
	case strings.HasPrefix(d, "alert:"): return b.alertCB(c, d, ad)
	case strings.HasPrefix(d, "admin:"): return b.adminCB(c, d, ad)
	}
	return nil
}

func (b *Bot) onText(c tele.Context) error {
	uid := c.Sender().ID; b.mu.RLock(); act, ok := b.pend[uid]; b.mu.RUnlock()
	if !ok || act != "allow" { return nil }
	ps := strings.SplitN(c.Text(), " ", 2)
	if len(ps) < 2 { return c.Send("Format: ID Nom") }
	b.mu.Lock(); delete(b.pend, uid); b.mu.Unlock()
	return c.Send("Utilisateur autorise.", menuKB("admin", true))
}

func (b *Bot) draftCB(c tele.Context, d string, ad bool) error {
	uid := c.Sender().ID
	switch {
	case d == "draft:start":
		b.mu.Lock(); b.drafts[uid] = newDraft(); b.mu.Unlock()
		dr := b.draft(uid); return c.Edit(fDraft(dr), draftKB(dr, ad))
	case d == "draft:cancel":
		b.mu.Lock(); delete(b.drafts, uid); b.mu.Unlock()
		return c.Edit("Creation annulee.", homeKB(ad))
	case d == "draft:next":
		dr := b.draft(uid); dr.step = nxtStep(dr.step); return c.Edit(fDraft(dr), draftKB(dr, ad))
	case d == "draft:prev":
		dr := b.draft(uid); dr.step = prvStep(dr.step); return c.Edit(fDraft(dr), draftKB(dr, ad))
	case strings.HasPrefix(d, "draft:toggle:"):
		ps := strings.Split(d, ":"); dr := b.draft(uid)
		if len(ps) < 4 { break }
		switch ps[2] {
		case "media": dr.mediaTypes = tglV(dr.mediaTypes, ps[3])
		case "condition": dr.conditions = tglV(dr.conditions, ps[3])
		case "category": dr.driveCategories = tglV(dr.driveCategories, ps[3])
		case "interface": dr.interfaces = tglV(dr.interfaces, ps[3])
		}
		return c.Edit(fDraft(dr), draftKB(dr, ad))
	case strings.HasPrefix(d, "draft:cap:"):
		k := d[strings.LastIndex(d,":")+1:]; dr := b.draft(uid)
		if p, ok := rules.CapacityPresets[k]; ok {
			if k == "all" { dr.capacityPresets = nil } else if hV(dr.capacityPresets, k) { dr.capacityPresets = tglV(dr.capacityPresets, k) } else { dr.capacityPresets = append(dr.capacityPresets, k) }
			if k != "all" && p.MediaType != "all" && !hV(dr.mediaTypes, p.MediaType) { dr.mediaTypes = append(dr.mediaTypes, p.MediaType) }
		}
		return c.Edit(fDraft(dr), draftKB(dr, ad))
	case strings.HasPrefix(d, "draft:price:"):
		k := d[strings.LastIndex(d,":")+1:]; dr := b.draft(uid)
		for _, pl := range priceLabels { if pl.Key == k { dr.maxPricePerTB = pl.Value; if pl.Media != "all" && !hV(dr.mediaTypes, pl.Media) { dr.mediaTypes = append(dr.mediaTypes, pl.Media) }; break } }
		return c.Edit(fDraft(dr), draftKB(dr, ad))
	case d == "draft:create":
		dr := b.draft(uid); nm := dr.name; if nm == "" { nm = "Alerte DiskCount" }
		a, err := b.db.CreateAlert(context.Background(), c.Chat().ID, uid, nm, dr.maxPricePerTB, 5.0, 24, dr.capacityPresets, dr.conditions, dr.mediaTypes, dr.driveCategories, dr.interfaces, dr.sources)
		if err != nil { return c.Edit("Erreur: "+err.Error(), draftKB(dr, ad)) }
		b.mu.Lock(); delete(b.drafts, uid); b.mu.Unlock()
		return c.Edit(aStr(a), editKB(a))
	}
	return nil
}

func (b *Bot) alertCB(c tele.Context, d string, ad bool) error {
	ps := strings.Split(d, ":"); act := ps[1]; aID, _ := strconv.ParseInt(ps[2], 10, 64); uid := c.Sender().ID
	a, _ := b.db.GetAlert(context.Background(), uid, aID); if a == nil { return c.Send("Alerte introuvable.") }
	as := func() string { return aStr(a) }

	switch act {
	case "edit": return c.Edit(as(), editKB(a))
	case "enabled":
		b.db.SetAlertEnabled(context.Background(), uid, aID, !a.Enabled)
		if a2, _ := b.db.GetAlert(context.Background(), uid, aID); a2 != nil { a = a2 }
		return c.Edit(as(), editKB(a))
	case "delete":
		return c.Edit("Supprimer #"+ps[2], &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
			{ib("Confirmer suppression #"+ps[2], "alert:delete_confirm:"+ps[2])},
			{ib("Precedent", "alert:edit:"+ps[2]), ib("Accueil", "menu:home")},
		}})
	case "delete_confirm":
		b.db.DeleteAlert(context.Background(), uid, aID)
		as, _ := b.db.GetAlertsByOwner(context.Background(), uid, false)
		return c.Edit("Supprimee.", alertsKB(as, ad))
	case "media": return c.Edit("Type\n\n"+as(), alTglKB("media", aID, [][2]string{{"HDD","rotational"},{"SSD","solid_state"}}, a.MediaTypes))
	case "condition": return c.Edit("Etat\n\n"+as(), alTglKB("condition", aID, [][2]string{{"New","new"},{"Used","used"}}, a.Conditions))
	case "capacity": return c.Edit("Capacite\n\n"+as(), alCapKB(a))
	case "price": return c.Edit("Prix\n\n"+as(), alPriceKB(a))
	case "categories": return c.Edit("Categories\n\n"+as(), alTglKB("category", aID, catPairs, a.DriveCategories))
	case "interfaces": return c.Edit("Connexions\n\n"+as(), alTglKB("interface", aID, ifacePairs, a.Interfaces))
	case "toggle":
		if len(ps) >= 5 { b.db.ToggleAlertFilter(context.Background(), uid, aID, ps[3], ps[4])
			if a2, _ := b.db.GetAlert(context.Background(), uid, aID); a2 != nil { a = a2 }
			switch ps[3] {
			case "media": return c.Edit("Type\n\n"+as(), alTglKB("media", aID, [][2]string{{"HDD","rotational"},{"SSD","solid_state"}}, a.MediaTypes))
			case "condition": return c.Edit("Etat\n\n"+as(), alTglKB("condition", aID, [][2]string{{"New","new"},{"Used","used"}}, a.Conditions))
			case "category": return c.Edit("Categories\n\n"+as(), alTglKB("category", aID, catPairs, a.DriveCategories))
			case "interface": return c.Edit("Connexions\n\n"+as(), alTglKB("interface", aID, ifacePairs, a.Interfaces))
			}
		}
	case "cap":
		if len(ps) >= 4 { k := ps[3]; if _, ok := rules.CapacityPresets[k]; ok {
			var nps []string
			if k == "all" { nps = nil } else if hV(a.CapacityPresets, k) { nps = tglV(a.CapacityPresets, k) } else { nps = append(a.CapacityPresets, k) }
			b.db.UpdateAlertCaps(context.Background(), uid, aID, nps)
			a.CapacityPresets = nps
		}}
	case "price_set":
		if len(ps) >= 4 { k := ps[3]
			for _, pl := range priceLabels { if pl.Key == k {
				b.db.SetAlertMaxPrice(context.Background(), uid, aID, pl.Value)
				if pl.Value != nil { a.MaxPricePerTB = pl.Value } else { a.MaxPricePerTB = nil }
				if pl.Media != "all" { b.db.ToggleAlertFilter(context.Background(), uid, aID, "media", pl.Media) }; break
			}}
		}
	}
	return c.Edit(as(), editKB(a))
}

func (b *Bot) adminCB(c tele.Context, d string, ad bool) error {
	switch d {
	case "admin:list":
		us, _ := b.db.ListAuthorizedUsers(context.Background(), true)
		var ls []string; for _, u := range us { ls = append(ls, fmt.Sprintf("%s | %d", u.Label, u.TelegramUserID)) }
		return c.Edit("Utilisateurs\n\n"+strings.Join(ls,"\n"), menuKB("admin", true))
	case "admin:add":
		b.mu.Lock(); b.pend[c.Sender().ID] = "allow"; b.mu.Unlock()
		return c.Edit("Ajouter\n\nEnvoie: 123456789 Nom custom", menuKB("admin", true))
	case "admin:revoke": return c.Edit("Commande: /revoke ID", menuKB("admin", true))
	}
	return nil
}

func (b *Bot) Run(ctx context.Context) error {
	slog.Info("bot starting"); go b.TB.Start(); <-ctx.Done(); b.TB.Stop(); return nil
}

func nxtStep(s string) string  { for i, v := range wizardSteps { if v == s { return wizardSteps[min(i+1, len(wizardSteps)-1)] } }; return s }
func prvStep(s string) string  { for i, v := range wizardSteps { if v == s { return wizardSteps[max(i-1, 0)] } }; return s }
func tglV(l []string, v string) []string { for i, x := range l { if x == v { return append(l[:i], l[i+1:]...) } }; return append(l, v) }
func hV(l []string, v string) bool        { for _, x := range l { if x == v { return true } }; return false }

var mHome = "DiskCount\n\nChoisis une action.\n\nCreer une alerte lance le wizard complet. Mes alertes ouvre tes alertes pour les modifier, les pauser ou les supprimer. Scanner/Test verifie le bot sans envoyer de notification."

func aStr(a *db.Alert) string {
	st := "on"; if !a.Enabled { st = "off" }; c := fmCap(a); p := ""
	if a.MaxPricePerTB != nil { p = fmt.Sprintf("prix<=%.0fEUR/To", *a.MaxPricePerTB) }
	return fmt.Sprintf("#%d [%s] %s | capacite=%s | %s | remise>=%.0f%%", a.ID, st, a.Name, c, p, a.MinDiscountPct)
}
func fmCap(a *db.Alert) string {
	if len(a.CapacityPresets) > 0 {
		var ns []string; for _, k := range a.CapacityPresets { if p, ok := rules.CapacityPresets[k]; ok { ns = append(ns, p.Label) } }
		if len(ns) > 0 { return strings.Join(ns, ", ") }
	}
	return "toute capacite"
}

func fDraft(d *alertDraft) string {
	t := map[string]string{"media":"1/8 Type de disque","condition":"2/8 Etat produit","capacity":"3/8 Capacite","price":"4/8 Prix","categories":"5/8 Categories","interfaces":"6/8 Connexions","confirm":"7/7 Recapitulatif"}
	h := map[string]string{"media":"Choisis HDD, SSD, ou les deux.","condition":"Choisis New, Used, ou les deux.","capacity":"Selectionne des plages.","price":"Choisis un prix max.","categories":"Filtre les familles.","interfaces":"Filtre les connexions.","confirm":"Verifie puis cree."}
	return fmt.Sprintf("%s\n\n%s\n\n%s", t[d.step], fDS(d), h[d.step])
}
func fDS(d *alertDraft) string {
	c := "toute capacite"; if len(d.capacityPresets) > 0 { var ns []string; for _, k := range d.capacityPresets { if p, ok := rules.CapacityPresets[k]; ok { ns = append(ns, p.Label) } }; if len(ns) > 0 { c = strings.Join(ns, ", ") } }
	pr := "aucune limite"; if d.maxPricePerTB != nil { pr = fmt.Sprintf("%.0f EUR/To", *d.maxPricePerTB) }
	return fmt.Sprintf("Nom: %s\nType: %s\nEtat: %s\nCapacite: %s\nPrix max: %s\nCategories: %s\nConnexions: %s", d.name, fv(d.mediaTypes), fv(d.conditions), c, pr, fv(d.driveCategories), fv(d.interfaces))
}
func fv(v []string) string { if len(v) == 0 { return "tous" }; var ls []string; for _, x := range v { ls = append(ls, dv(x)) }; return strings.Join(ls, ", ") }
func dv(v string) string {
	m := map[string]string{"rotational":"HDD","solid_state":"SSD","new":"New","used":"Used","external_3_5":"External 3.5","external_2_5":"External 2.5","internal_3_5":"Internal 3.5","internal_2_5":"Internal 2.5","internal_hybrid":"Hybrid","internal_sas":"Internal SAS","external_ssd":"External SSD","internal_ssd":"Internal SSD","m2_sata":"M.2 SATA","m2_nvme":"M.2 NVMe","u2_u3":"U.2/U.3","sata":"SATA","sas":"SAS","nvme":"NVMe","usb":"USB"}
	if l, ok := m[v]; ok { return l }; return v
}

func ib(t, u string) tele.InlineButton { return tele.InlineButton{Text: t, Unique: u} }

func homeKB(ad bool) *tele.ReplyMarkup {
	rows := [][]tele.InlineButton{{ib("Creer une alerte","draft:start")},{ib("Mes alertes","menu:alerts:list"),ib("Scanner/Test","menu:scan")},{ib("Aide","menu:help")}}
	if ad { rows = append(rows, []tele.InlineButton{ib("Admin","menu:admin")}) }
	return &tele.ReplyMarkup{InlineKeyboard: rows}
}

func menuKB(v string, ad bool) *tele.ReplyMarkup {
	nav := [][]tele.InlineButton{{ib("Precedent","menu:"+mp(v)), ib("Accueil","menu:home")}}
	switch {
	case v == "home": return homeKB(ad)
	case v == "scan": return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("Statut","menu:scan:status"),ib("Test","menu:scan:test")},nav[0]}}
	case v == "help": return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("Creer","menu:help:create"),ib("Alertes","menu:help:alerts")},{ib("Capacites","menu:help:capacity"),ib("Prix","menu:help:price")},{ib("Categories","menu:help:categories"),ib("Connexions","menu:help:interfaces")},{ib("Scanner","menu:help:scan"),ib("Admin","menu:help:admin")},{ib("Commdes","menu:help:commands"),ib("Filtres","menu:help:filters")},nav[0]}}
	case v == "admin": return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("Liste","admin:list"),ib("Ajouter","admin:add")},{ib("Revoquer","admin:revoke")},nav[0]}}
	default: return &tele.ReplyMarkup{InlineKeyboard: nav}
	}
}
func mp(v string) string { if !strings.Contains(v,":") { return "home" }; return v[:strings.LastIndex(v,":")] }

func alertsKB(as []db.Alert, ad bool) *tele.ReplyMarkup {
	var r [][]tele.InlineButton
	for _, a := range as { r = append(r, []tele.InlineButton{ib(fAb(&a),"alert:edit:"+is(a.ID))}) }
	r = append(r, []tele.InlineButton{ib("Creer une alerte","draft:start")})
	r = append(r, []tele.InlineButton{ib("Precedent","menu:alerts:list"),ib("Accueil","menu:home")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func fAb(a *db.Alert) string {
	st := "on"; if !a.Enabled { st = "off" }; m := "HDD/SSD"; if len(a.MediaTypes) > 0 { m = strings.Join(a.MediaTypes,",") }
	pr := "prix libre"; if a.MaxPricePerTB != nil { pr = fmt.Sprintf("prix<=%.0fEUR/To",*a.MaxPricePerTB) }
	return fmt.Sprintf("#%d %s | %s | %s | %s | %s", a.ID, a.Name, st, m, fmCap(a), pr)
}
func editKB(a *db.Alert) *tele.ReplyMarkup {
	sl := "Pauser"; if !a.Enabled { sl = "Reprendre" }; s := is(a.ID)
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
		{ib(sl,"alert:enabled:"+s)},
		{ib("Type","alert:media:"+s),ib("Etat","alert:condition:"+s)},
		{ib("Capacite","alert:capacity:"+s),ib("Prix","alert:price:"+s)},
		{ib("Categories","alert:categories:"+s),ib("Connexions","alert:interfaces:"+s)},
		{ib("Supprimer","alert:delete:"+s)},
		{ib("Precedent","menu:alerts:list"),ib("Accueil","menu:home")},
	}}
}
func is(id int64) string { return strconv.FormatInt(id, 10) }

func alTglKB(f string, id int64, pairs [][2]string, sel []string) *tele.ReplyMarkup {
	var r [][]tele.InlineButton; var c []tele.InlineButton
	for _, p := range pairs {
		c = append(c, ib(lTgl(p[0],p[1],sel), fmt.Sprintf("alert:toggle:%d:%s:%s",id,f,p[1])))
		if len(c) == 2 { r = append(r, c); c = nil }
	}
	if len(c) > 0 { r = append(r, c) }
	r = append(r, []tele.InlineButton{ib("Precedent",fmt.Sprintf("alert:edit:%d",id)),ib("Accueil","menu:home")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func lTgl(l, v string, sel []string) string { if hV(sel, v) { return "[x] " + l }; return "[ ] " + l }

func lPres(k string, sel []string) string {
	p, ok := rules.CapacityPresets[k]; if !ok { return "[ ] ??" }
	if k == "all" && len(sel) == 0 { return "[x] " + p.Label }
	if hV(sel, k) { return "[x] " + p.Label }; return "[ ] " + p.Label
}

func alCapKB(a *db.Alert) *tele.ReplyMarkup {
	s := is(a.ID)
	r := [][]tele.InlineButton{{ib(lPres("all",a.CapacityPresets), "alert:cap:"+s+":all")}}
	r = append(r, prRows("alert:cap:"+s, hddCapKeys, a.CapacityPresets)...)
	r = append(r, prRows("alert:cap:"+s, ssdCapKeys, a.CapacityPresets)...)
	r = append(r, []tele.InlineButton{ib("Precedent","alert:edit:"+s),ib("Accueil","menu:home")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func prRows(p string, ks []string, sel []string) [][]tele.InlineButton {
	var r [][]tele.InlineButton; var c []tele.InlineButton
	for _, k := range ks { c = append(c, ib(lPres(k,sel), p+":"+k)); if len(c) == 2 { r = append(r, c); c = nil } }
	if len(c) > 0 { r = append(r, c) }; return r
}

func alPriceKB(a *db.Alert) *tele.ReplyMarkup {
	s := is(a.ID); kk := prKey(a); r := [][]tele.InlineButton{{ib("Aucune limite","alert:price_set:"+s+":none")}}
	pr := func(ks []string) {
		var c []tele.InlineButton
		for _, k := range ks {
			for _, pl := range priceLabels { if pl.Key == k { l := pl.Label; if k == kk { l = "[x] "+l }; c = append(c, ib(l,"alert:price_set:"+s+":"+k)); break } }
			if len(c) == 2 { r = append(r, c); c = nil }
		}
		if len(c) > 0 { r = append(r, c) }
	}
	pr(hddPrKeys); pr(ssdPrKeys)
	r = append(r, []tele.InlineButton{ib("Precedent","alert:edit:"+s),ib("Accueil","menu:home")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func prKey(a *db.Alert) string {
	if a.MaxPricePerTB == nil { return "none" }
	for _, pl := range priceLabels { if pl.Value != nil && fEq(*pl.Value, *a.MaxPricePerTB) { return pl.Key } }; return ""
}
func fEq(a, b float64) bool { d := a - b; if d < 0 { d = -d }; return d < 0.01 }

func draftKB(d *alertDraft, ad bool) *tele.ReplyMarkup {
	nav := []tele.InlineButton{ib("Precedent","draft:prev"),ib("Suivant","draft:next"),ib("Accueil","menu:home")}
	g := func(rows ...[]tele.InlineButton) *tele.ReplyMarkup { return &tele.ReplyMarkup{InlineKeyboard: rows} }
	switch d.step {
	case "media":
		return g([]tele.InlineButton{ib(lTgl("HDD","rotational",d.mediaTypes),"draft:toggle:media:rotational"),ib(lTgl("SSD","solid_state",d.mediaTypes),"draft:toggle:media:solid_state")},nav)
	case "condition":
		return g([]tele.InlineButton{ib(lTgl("New","new",d.conditions),"draft:toggle:condition:new"),ib(lTgl("Used","used",d.conditions),"draft:toggle:condition:used")},nav)
	case "capacity":
		r := [][]tele.InlineButton{{ib(lPres("all",d.capacityPresets),"draft:cap:all")}}
		r = append(r, prRows("draft:cap",hddCapKeys,d.capacityPresets)...)
		r = append(r, prRows("draft:cap",ssdCapKeys,d.capacityPresets)...)
		r = append(r, nav)
		return g(r...)
	case "price":
		r := [][]tele.InlineButton{{ib("Aucune limite","draft:price:none")}}
		kk := ""
		if d.maxPricePerTB != nil { for _, pl := range priceLabels { if pl.Value != nil && fEq(*pl.Value,*d.maxPricePerTB) { kk = pl.Key; break } } }
		pr := func(ks []string) {
			var c []tele.InlineButton
			for _, k := range ks { for _, pl := range priceLabels { if pl.Key == k { l := pl.Label; if k == kk { l = "[x] "+l }; c = append(c, ib(l,"draft:price:"+k)); break } }; if len(c) == 2 { r = append(r, c); c = nil } }
			if len(c) > 0 { r = append(r, c) }
		}
		pr(hddPrKeys); pr(ssdPrKeys); r = append(r, nav); return g(r...)
	case "categories":
		return g(append(optRows(catPairs, d.driveCategories, "draft:toggle:category"), nav)...)
	case "interfaces":
		return g(append(optRows(ifacePairs, d.interfaces, "draft:toggle:interface"), nav)...)
	default:
		return g([]tele.InlineButton{ib("Creer","draft:create")}, []tele.InlineButton{ib("Precedent","draft:prev"),ib("Annuler","draft:cancel")}, []tele.InlineButton{ib("Accueil","menu:home")})
	}
}
func optRows(ps [][2]string, sel []string, px string) [][]tele.InlineButton {
	var r [][]tele.InlineButton; var c []tele.InlineButton
	for _, p := range ps { c = append(c, ib(lTgl(p[0],p[1],sel), px+":"+p[1])); if len(c) == 2 { r = append(r, c); c = nil } }
	if len(c) > 0 { r = append(r, c) }; return r
}

func parseKV(t string) map[string]string {
	r := make(map[string]string)
	for _, p := range strings.Fields(t) {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 { r[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1]) }
	}
	return r
}
func flds(s string) []string { if s == "" { return nil }; return strings.Fields(s) }

var _ = slog.Info
