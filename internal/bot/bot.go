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

var wizardSteps = []string{"media", "condition", "capacity", "price", "categories", "interfaces", "brand", "recording", "confirm"}

type alertDraft struct {
	step             string
	name             string
	capacityPresets  []string
	maxPricePerTB    *float64
	conditions       []string
	mediaTypes       []string
	driveCategories  []string
	interfaces       []string
	sources          []string
	brands           []string
	recordingMethods []string
	updatedAt        time.Time
}

type Bot struct {
	TB      *tele.Bot
	db      *db.DB
	cfg     *config.Config
	scanner *scanner.Scanner
	drafts  map[int64]*alertDraft
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

var brandPairs = [][2]string{
	{"Seagate", "Seagate"}, {"Western Digital", "Western Digital"}, {"WD", "WD"},
	{"Toshiba", "Toshiba"}, {"Samsung", "Samsung"}, {"Crucial", "Crucial"},
	{"Kingston", "Kingston"}, {"HGST", "HGST"}, {"Micron", "Micron"},
}

var recordingPairs = [][2]string{{"CMR", "cmr"}, {"SMR", "smr"}}

var sourcePairs = [][2]string{
	{"diskprices", "diskprices"}, {"pricepergig", "pricepergig"},
	{"pricepertb", "pricepertb"}, {"dealabs", "dealabs"},
	{"idealo", "idealo"}, {"ledenicheur", "ledenicheur"},
	{"leboncoin", "leboncoin"},
}

func pf(f float64) *float64 { return &f }

func New(cfg *config.Config, dbase *db.DB, scan *scanner.Scanner) (*Bot, error) {
	tb, err := tele.NewBot(tele.Settings{Token: cfg.TelegramBotToken, Poller: &tele.LongPoller{Timeout: 10 * time.Second}})
	if err != nil {
		return nil, err
	}
	b := &Bot{TB: tb, db: dbase, cfg: cfg, scanner: scan, drafts: make(map[int64]*alertDraft)}
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
	b.TB.Handle("/set_keywords", b.auth(b.setKeywordsCmd))
	b.TB.Handle("/prices", b.auth(b.pricesCmd))
	b.TB.Handle(tele.OnCallback, b.callback)

	b.TB.SetCommands(botCommands())
}

func botCommands() []tele.Command {
	return []tele.Command{
		{Text: "start", Description: "Demarrer le bot"}, {Text: "menu", Description: "Navigation par tuiles"},
		{Text: "create", Description: "Creer une alerte (tuiles)"}, {Text: "help", Description: "Aide et filtres"},
		{Text: "add", Description: "Ajouter une alerte (texte)"}, {Text: "alerts", Description: "Lister tes alertes"},
		{Text: "pause", Description: "Mettre en pause"}, {Text: "resume", Description: "Reactiver"},
		{Text: "delete", Description: "Supprimer"}, {Text: "set_max_price", Description: "Modifier seuil EUR/To"},
		{Text: "set_capacity", Description: "Modifier capacite"}, {Text: "set_keywords", Description: "Modifier mots-cles"}, {Text: "prices", Description: "Voir les meilleurs prix actuels"},
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
func (b *Bot) allowed(uid int64) bool { return b.userOk(uid) }
func (b *Bot) userOk(uid int64) bool {
	ok, _ := b.db.IsUserAllowed(context.Background(), uid)
	return ok
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

func (b *Bot) start(c tele.Context) error {
	b.db.UpsertSubscriber(context.Background(), c.Chat().ID, nil)
	return c.Send(mHome, homeKB())
}
func (b *Bot) menu(c tele.Context) error { return c.Send(mHome, homeKB()) }
func (b *Bot) help(c tele.Context) error {
	return c.Send("Aide\n\nChoisis un theme.", menuKB("help"))
}

func (b *Bot) alertsCmd(c tele.Context) error {
	as, _ := b.db.GetAlertsByOwner(context.Background(), c.Sender().ID, false)
	if len(as) == 0 {
		return c.Send("Aucune alerte. Utilise /create.", menuKB("home"))
	}
	var ls []string
	for _, a := range as {
		ls = append(ls, aStr(&a))
	}
	return c.Send("Mes alertes\n\n"+strings.Join(ls, "\n"), alertsKB(as))
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
	a, err := b.db.CreateAlert(context.Background(), c.Chat().ID, c.Sender().ID, name, db.AlertDraft{
		MaxPricePerTB:  mx,
		MinDiscountPct: 5.0,
		CooldownHours:  24,
		CapacityPresets: caps,
		Conditions:     conds,
		MediaTypes:     meds,
	})
	if err != nil {
		return c.Send("Erreur: " + err.Error())
	}
	return c.Send(aStr(a), editKB(a))
}
func (b *Bot) pauseCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 {
		return c.Send("Usage: /pause ID")
	}
	b.db.SetAlertEnabled(context.Background(), c.Sender().ID, id, false)
	return c.Send(fmt.Sprintf("Alerte #%d en pause.", id))
}
func (b *Bot) resumeCmd(c tele.Context) error {
	id, _ := strconv.ParseInt(strings.TrimSpace(c.Message().Payload), 10, 64)
	if id == 0 {
		return c.Send("Usage: /resume ID")
	}
	b.db.SetAlertEnabled(context.Background(), c.Sender().ID, id, true)
	return c.Send(fmt.Sprintf("Alerte #%d activee.", id))
}
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
func (b *Bot) setCapacityCmd(c tele.Context) error {
	parts := strings.Fields(c.Message().Payload)
	if len(parts) < 2 {
		return c.Send("Usage: /set_capacity ID hdd_16_20,hdd_20_24  (ou ID none pour toute capacite)")
	}
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	if id == 0 {
		return c.Send("ID invalide.")
	}
	var presets []string
	if parts[1] != "none" {
		presets = strings.Split(parts[1], ",")
		for i := range presets {
			presets[i] = strings.TrimSpace(presets[i])
		}
		// Validate that presets exist.
		for _, k := range presets {
			if _, ok := rules.CapacityPresets[k]; !ok {
				return c.Send("Preset inconnu: " + k)
			}
		}
	}
	if err := b.db.UpdateAlertCaps(context.Background(), c.Sender().ID, id, presets); err != nil {
		return c.Send("Erreur: " + err.Error())
	}
	return c.Send(fmt.Sprintf("Capacite de l'alerte #%d mise a jour.", id))
}
func (b *Bot) setKeywordsCmd(c tele.Context) error {
	parts := strings.Fields(c.Message().Payload)
	if len(parts) < 2 {
		return c.Send("Usage: /set_keywords ID include:Exos,IronWolf exclude:archive\nOu: /set_keywords ID none")
	}
	id, _ := strconv.ParseInt(parts[0], 10, 64)
	if id == 0 {
		return c.Send("ID invalide.")
	}
	var include, exclude []string
	for _, p := range parts[1:] {
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		vals := strings.Split(kv[1], ",")
		for i := range vals {
			vals[i] = strings.TrimSpace(vals[i])
		}
		switch strings.ToLower(kv[0]) {
		case "include":
			include = vals
		case "exclude":
			exclude = vals
		}
	}
	if strings.ToLower(parts[1]) == "none" {
		include = nil
		exclude = nil
	}
	if err := b.db.SetAlertKeywords(context.Background(), c.Sender().ID, id, include, exclude); err != nil {
		return c.Send("Erreur: " + err.Error())
	}
	return c.Send(fmt.Sprintf("Mots-cles de l'alerte #%d mis a jour.", id))
}
func (b *Bot) createCmd(c tele.Context) error {
	uid := c.Sender().ID
	b.mu.Lock()
	b.drafts[uid] = newDraft()
	b.mu.Unlock()
	d := b.draft(uid)
	return c.Send(fDraft(d), draftKB(d))
}

func (b *Bot) pricesCmd(c tele.Context) error {
	return c.Send(b.currentPricesText(), pricesKB())
}

func (b *Bot) callback(c tele.Context) error {
	if !b.allowed(c.Sender().ID) {
		c.Respond()
		return nil
	}
	d := normalizeCallbackData(c.Callback().Data)
	c.Respond()

	switch {
	case d == "menu:home":
		return c.Edit(mHome, homeKB())
	case strings.HasPrefix(d, "menu:"):
		v := strings.TrimPrefix(d, "menu:")
		if v == "alerts:list" {
			as, _ := b.db.GetAlertsByOwner(context.Background(), c.Sender().ID, false)
			if len(as) == 0 {
				return c.Edit("Aucune alerte.", menuKB("home"))
			}
			var ls []string
			for _, a := range as {
				ls = append(ls, aStr(&a))
			}
			return c.Edit("Mes alertes\n\n"+strings.Join(ls, "\n"), alertsKB(as))
		}
		if v == "prices" || v == "prices:refresh" || v == "scan" || v == "scan:test" || v == "scan:status" {
			return c.Edit(b.currentPricesText(), pricesKB())
		}
		return c.Edit(helpText(v), menuKB(v))

	case strings.HasPrefix(d, "draft:"):
		return b.draftCB(c, d)
	case strings.HasPrefix(d, "alert:"):
		return b.alertCB(c, d)
	}
	return nil
}

func normalizeCallbackData(raw string) string {
	return strings.TrimPrefix(raw, "\f")
}

func (b *Bot) draftCB(c tele.Context, d string) error {
	uid := c.Sender().ID
	switch {
	case d == "draft:start":
		b.mu.Lock()
		b.drafts[uid] = newDraft()
		b.mu.Unlock()
		dr := b.draft(uid)
		return c.Edit(fDraft(dr), draftKB(dr))
	case d == "draft:cancel":
		b.mu.Lock()
		delete(b.drafts, uid)
		b.mu.Unlock()
		return c.Edit("Creation annulee.", homeKB())
	case d == "draft:next":
		dr := b.draft(uid)
		dr.step = nxtStep(dr.step)
		return c.Edit(fDraft(dr), draftKB(dr))
	case d == "draft:prev":
		dr := b.draft(uid)
		dr.step = prvStep(dr.step)
		return c.Edit(fDraft(dr), draftKB(dr))
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
			case "brand":
				dr.brands = tglV(dr.brands, ps[3])
			case "recording":
				dr.recordingMethods = tglV(dr.recordingMethods, ps[3])
			case "source":
				dr.sources = tglV(dr.sources, ps[3])
			}
		return c.Edit(fDraft(dr), draftKB(dr))
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
		return c.Edit(fDraft(dr), draftKB(dr))
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
		return c.Edit(fDraft(dr), draftKB(dr))
	case d == "draft:create":
		dr := b.draft(uid)
		nm := dr.name
		if nm == "" {
			nm = "Alerte DiskCount"
		}
		a, err := b.db.CreateAlert(context.Background(), c.Chat().ID, uid, nm, db.AlertDraft{
			MaxPricePerTB:    dr.maxPricePerTB,
			MinDiscountPct:   5.0,
			CooldownHours:    24,
			CapacityPresets:  dr.capacityPresets,
			Conditions:       dr.conditions,
			MediaTypes:       dr.mediaTypes,
			DriveCategories:  dr.driveCategories,
			Interfaces:       dr.interfaces,
			Sources:          dr.sources,
			Brands:           dr.brands,
			RecordingMethods: dr.recordingMethods,
		})
		if err != nil {
			return c.Edit("Erreur: "+err.Error(), draftKB(dr))
		}
		b.mu.Lock()
		delete(b.drafts, uid)
		b.mu.Unlock()
		return c.Edit(aStr(a), editKB(a))
	}
	return nil
}

func (b *Bot) alertCB(c tele.Context, d string) error {
	ps := strings.Split(d, ":")
	act := ps[1]
	aID, _ := strconv.ParseInt(ps[2], 10, 64)
	uid := c.Sender().ID
	a, _ := b.db.GetAlert(context.Background(), uid, aID)
	if a == nil {
		return c.Send("Alerte introuvable.")
	}
	as := func() string { return aStr(a) }

	switch act {
	case "edit":
		return c.Edit(as(), editKB(a))
	case "enabled":
		b.db.SetAlertEnabled(context.Background(), uid, aID, !a.Enabled)
		if a2, _ := b.db.GetAlert(context.Background(), uid, aID); a2 != nil {
			a = a2
		}
		return c.Edit(as(), editKB(a))
	case "delete":
		return c.Edit("Supprimer #"+ps[2], &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
			{ib("Confirmer suppression #"+ps[2], "alert:delete_confirm:"+ps[2])},
			{ib("Precedent", "alert:edit:"+ps[2]), ib("Accueil", "menu:home")},
		}})
	case "delete_confirm":
		b.db.DeleteAlert(context.Background(), uid, aID)
		as, _ := b.db.GetAlertsByOwner(context.Background(), uid, false)
		return c.Edit("Supprimee.", alertsKB(as))
	case "media":
		return c.Edit("Type\n\n"+as(), alTglKB("media", aID, [][2]string{{"HDD", "rotational"}, {"SSD", "solid_state"}}, a.MediaTypes))
	case "condition":
		return c.Edit("Etat\n\n"+as(), alTglKB("condition", aID, [][2]string{{"New", "new"}, {"Used", "used"}}, a.Conditions))
	case "capacity":
		return c.Edit("Capacite\n\n"+as(), alCapKB(a))
	case "price":
		return c.Edit("Prix\n\n"+as(), alPriceKB(a))
	case "categories":
		return c.Edit("Categories\n\n"+as(), alTglKB("category", aID, catPairs, a.DriveCategories))
	case "interfaces":
		return c.Edit("Connexions\n\n"+as(), alTglKB("interface", aID, ifacePairs, a.Interfaces))
	case "brand":
		return c.Edit("Marque\n\n"+as(), alTglKB("brand", aID, brandPairs, a.Brands))
	case "recording":
		return c.Edit("Enregistrement\n\n"+as(), alTglKB("recording_method", aID, recordingPairs, a.RecordingMethods))
	case "source":
		return c.Edit("Sources\n\n"+as(), alTglKB("source", aID, sourcePairs, a.Sources))
	case "toggle":
		if len(ps) >= 5 {
			b.db.ToggleAlertFilter(context.Background(), uid, aID, ps[3], ps[4])
			if a2, _ := b.db.GetAlert(context.Background(), uid, aID); a2 != nil {
				a = a2
			}
			switch ps[3] {
			case "media":
				return c.Edit("Type\n\n"+as(), alTglKB("media", aID, [][2]string{{"HDD", "rotational"}, {"SSD", "solid_state"}}, a.MediaTypes))
			case "condition":
				return c.Edit("Etat\n\n"+as(), alTglKB("condition", aID, [][2]string{{"New", "new"}, {"Used", "used"}}, a.Conditions))
			case "category":
				return c.Edit("Categories\n\n"+as(), alTglKB("category", aID, catPairs, a.DriveCategories))
			case "interface":
				return c.Edit("Connexions\n\n"+as(), alTglKB("interface", aID, ifacePairs, a.Interfaces))
			case "brand":
				return c.Edit("Marque\n\n"+as(), alTglKB("brand", aID, brandPairs, a.Brands))
			case "recording_method":
				return c.Edit("Enregistrement\n\n"+as(), alTglKB("recording_method", aID, recordingPairs, a.RecordingMethods))
			case "source":
				return c.Edit("Sources\n\n"+as(), alTglKB("source", aID, sourcePairs, a.Sources))
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
	}
	return c.Edit(as(), editKB(a))
}

func (b *Bot) Run(ctx context.Context) error {
	slog.Info("bot starting")
	go b.TB.Start()
	<-ctx.Done()
	b.TB.Stop()
	return nil
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

var mHome = "DiskCount\n\nChoisis une action.\n\nCreer une alerte lance le wizard complet. Mes alertes ouvre tes alertes pour les modifier, les pauser ou les supprimer. Prix actuels affiche les meilleurs prix connus depuis le dernier scan automatique."

func helpText(v string) string {
	switch v {
	case "help", "help:commands":
		return "Aide\n\nCommandes utiles: /create, /alerts, /prices, /pause ID, /resume ID, /delete ID.\n\nLa gestion des utilisateurs autorises se fait uniquement dans l'interface Web."
	case "help:create":
		return "Creer une alerte\n\nUtilise /create ou le bouton Creer une alerte. Le wizard te fait choisir type de disque, etat, capacite, prix, categorie et connexion."
	case "help:alerts":
		return "Mes alertes\n\nUtilise /alerts ou le bouton Mes alertes pour modifier, pauser, reprendre ou supprimer tes alertes."
	case "help:capacity":
		return "Capacites\n\nLes plages HDD et SSD servent a filtrer les offres par taille utile. Choisis une ou plusieurs plages, ou toute capacite."
	case "help:price":
		return "Prix\n\nLe seuil est exprime en EUR/To. Pour les SSD, les boutons en EUR/Go sont convertis en EUR/To."
	case "help:categories":
		return "Categories\n\nLes categories filtrent les formats: interne, externe, M.2, NVMe, SAS, etc."
	case "help:interfaces":
		return "Connexions\n\nLes connexions filtrent SATA, SAS, NVMe ou USB quand l'information est connue."
	case "help:prices":
		return "Prix actuels\n\nCette vue affiche les meilleurs prix deja connus en base depuis le dernier scan automatique. Elle ne lance pas de scan manuel et n'envoie aucune notification."
	case "help:filters":
		return "Filtres\n\nUne offre doit correspondre a tes filtres d'alerte et respecter le delai anti-doublon avant notification."
	default:
		return "Aide\n\nChoisis un theme."
	}
}

func (b *Bot) currentPricesText() string {
	prices, err := b.db.LatestPrices(context.Background(), 10)
	if err != nil {
		return "Prix actuels\n\nErreur DB: " + err.Error()
	}
	if len(prices) == 0 {
		return "Prix actuels\n\nAucun prix connu pour le moment. Les prix seront disponibles apres un scan automatique."
	}
	var lines []string
	lines = append(lines, "Prix actuels\n", "Meilleurs prix connus depuis le dernier scan automatique.\n")
	for i, p := range prices {
		title := p.Title
		if strings.TrimSpace(title) == "" {
			title = p.ProductID
		}
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, shortTitle(title, 72)))
		lines = append(lines, fmt.Sprintf("%.2f To | %.2f EUR | %.2f EUR/To | %s", p.CapacityTB, p.PriceEUR, p.PricePerTB, p.Source))
		if p.URL != "" {
			lines = append(lines, p.URL)
		}
		lines = append(lines, "")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func shortTitle(title string, maxLen int) string {
	title = strings.TrimSpace(strings.Join(strings.Fields(title), " "))
	if len(title) <= maxLen {
		return title
	}
	if maxLen <= 3 {
		return title[:maxLen]
	}
	return title[:maxLen-3] + "..."
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
	extras := ""
	if len(a.Brands) > 0 {
		extras += fmt.Sprintf(" | marque=%s", strings.Join(a.Brands, ","))
	}
	if len(a.RecordingMethods) > 0 {
		extras += fmt.Sprintf(" | %s", strings.Join(a.RecordingMethods, "/"))
	}
	if len(a.Keywords) > 0 {
		extras += fmt.Sprintf(" | +%s", strings.Join(a.Keywords, ","))
	}
	if len(a.ExcludeKeywords) > 0 {
		extras += fmt.Sprintf(" | -%s", strings.Join(a.ExcludeKeywords, ","))
	}
	if len(a.Sources) > 0 {
		extras += fmt.Sprintf(" | src=%s", strings.Join(a.Sources, ","))
	}
	return fmt.Sprintf("#%d [%s] %s | capacite=%s | %s | remise>=%.0f%%%s", a.ID, st, a.Name, c, p, a.MinDiscountPct, extras)
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
	t := map[string]string{"media": "1/9 Type de disque", "condition": "2/9 Etat produit", "capacity": "3/9 Capacite", "price": "4/9 Prix", "categories": "5/9 Categories", "interfaces": "6/9 Connexions", "brand": "7/9 Marque", "recording": "8/9 Enregistrement", "confirm": "9/9 Recapitulatif"}
	h := map[string]string{"media": "Choisis HDD, SSD, ou les deux.", "condition": "Choisis New, Used, ou les deux.", "capacity": "Selectionne des plages.", "price": "Choisis un prix max.", "categories": "Filtre les familles.", "interfaces": "Filtre les connexions.", "brand": "Filtre par marque (optionnel).", "recording": "CMR (NAS) ou SMR. Optionnel.", "confirm": "Verifie puis cree."}
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
	return fmt.Sprintf("Nom: %s\nType: %s\nEtat: %s\nCapacite: %s\nPrix max: %s\nCategories: %s\nConnexions: %s\nMarque: %s\nEnregistrement: %s", d.name, fv(d.mediaTypes), fv(d.conditions), c, pr, fv(d.driveCategories), fv(d.interfaces), fv(d.brands), fv(d.recordingMethods))
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

func ib(t, data string) tele.InlineButton { return tele.InlineButton{Text: t, Data: data} }

func homeKB() *tele.ReplyMarkup {
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("Creer une alerte", "draft:start")}, {ib("Mes alertes", "menu:alerts:list"), ib("Prix actuels", "menu:prices")}, {ib("Aide", "menu:help")}}}
}

func menuKB(v string) *tele.ReplyMarkup {
	nav := [][]tele.InlineButton{{ib("Precedent", "menu:"+mp(v)), ib("Accueil", "menu:home")}}
	switch {
	case v == "home":
		return homeKB()
	case v == "prices":
		return pricesKB()
	case v == "help":
		return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{ib("Creer", "menu:help:create"), ib("Alertes", "menu:help:alerts")}, {ib("Capacites", "menu:help:capacity"), ib("Prix", "menu:help:price")}, {ib("Categories", "menu:help:categories"), ib("Connexions", "menu:help:interfaces")}, {ib("Prix actuels", "menu:help:prices"), ib("Commandes", "menu:help:commands")}, {ib("Filtres", "menu:help:filters")}, nav[0]}}
	default:
		return &tele.ReplyMarkup{InlineKeyboard: nav}
	}
}

func pricesKB() *tele.ReplyMarkup {
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{
		{ib("Rafraichir", "menu:prices:refresh")},
		{ib("Accueil", "menu:home")},
	}}
}
func mp(v string) string {
	if !strings.Contains(v, ":") {
		return "home"
	}
	return v[:strings.LastIndex(v, ":")]
}

func alertsKB(as []db.Alert) *tele.ReplyMarkup {
	var r [][]tele.InlineButton
	for _, a := range as {
		r = append(r, []tele.InlineButton{ib(fAb(&a), "alert:edit:"+is(a.ID))})
	}
	r = append(r, []tele.InlineButton{ib("Creer une alerte", "draft:start")})
	r = append(r, []tele.InlineButton{ib("Precedent", "menu:alerts:list"), ib("Accueil", "menu:home")})
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
		{ib(sl, "alert:enabled:"+s)},
		{ib("Type", "alert:media:"+s), ib("Etat", "alert:condition:"+s)},
		{ib("Capacite", "alert:capacity:"+s), ib("Prix", "alert:price:"+s)},
		{ib("Categories", "alert:categories:"+s), ib("Connexions", "alert:interfaces:"+s)},
		{ib("Marque", "alert:brand:"+s), ib("Enregistrement", "alert:recording:"+s)},
		{ib("Sources", "alert:source:"+s)},
		{ib("Supprimer", "alert:delete:"+s)},
		{ib("Precedent", "menu:alerts:list"), ib("Accueil", "menu:home")},
	}}
}
func is(id int64) string { return strconv.FormatInt(id, 10) }

func alTglKB(f string, id int64, pairs [][2]string, sel []string) *tele.ReplyMarkup {
	var r [][]tele.InlineButton
	var c []tele.InlineButton
	for _, p := range pairs {
		c = append(c, ib(lTgl(p[0], p[1], sel), fmt.Sprintf("alert:toggle:%d:%s:%s", id, f, p[1])))
		if len(c) == 2 {
			r = append(r, c)
			c = nil
		}
	}
	if len(c) > 0 {
		r = append(r, c)
	}
	r = append(r, []tele.InlineButton{ib("Precedent", fmt.Sprintf("alert:edit:%d", id)), ib("Accueil", "menu:home")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
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

func alCapKB(a *db.Alert) *tele.ReplyMarkup {
	s := is(a.ID)
	r := [][]tele.InlineButton{{ib(lPres("all", a.CapacityPresets), "alert:cap:"+s+":all")}}
	r = append(r, prRows("alert:cap:"+s, hddCapKeys, a.CapacityPresets)...)
	r = append(r, prRows("alert:cap:"+s, ssdCapKeys, a.CapacityPresets)...)
	r = append(r, []tele.InlineButton{ib("Precedent", "alert:edit:"+s), ib("Accueil", "menu:home")})
	return &tele.ReplyMarkup{InlineKeyboard: r}
}
func prRows(p string, ks []string, sel []string) [][]tele.InlineButton {
	var r [][]tele.InlineButton
	var c []tele.InlineButton
	for _, k := range ks {
		c = append(c, ib(lPres(k, sel), p+":"+k))
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
	r := [][]tele.InlineButton{{ib("Aucune limite", "alert:price_set:"+s+":none")}}
	pr := func(ks []string) {
		var c []tele.InlineButton
		for _, k := range ks {
			for _, pl := range priceLabels {
				if pl.Key == k {
					l := pl.Label
					if k == kk {
						l = "[x] " + l
					}
					c = append(c, ib(l, "alert:price_set:"+s+":"+k))
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
	r = append(r, []tele.InlineButton{ib("Precedent", "alert:edit:"+s), ib("Accueil", "menu:home")})
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

func draftKB(d *alertDraft) *tele.ReplyMarkup {
	nav := []tele.InlineButton{ib("Precedent", "draft:prev"), ib("Suivant", "draft:next"), ib("Accueil", "menu:home")}
	g := func(rows ...[]tele.InlineButton) *tele.ReplyMarkup { return &tele.ReplyMarkup{InlineKeyboard: rows} }
	switch d.step {
	case "media":
		return g([]tele.InlineButton{ib(lTgl("HDD", "rotational", d.mediaTypes), "draft:toggle:media:rotational"), ib(lTgl("SSD", "solid_state", d.mediaTypes), "draft:toggle:media:solid_state")}, nav)
	case "condition":
		return g([]tele.InlineButton{ib(lTgl("New", "new", d.conditions), "draft:toggle:condition:new"), ib(lTgl("Used", "used", d.conditions), "draft:toggle:condition:used")}, nav)
	case "capacity":
		r := [][]tele.InlineButton{{ib(lPres("all", d.capacityPresets), "draft:cap:all")}}
		r = append(r, prRows("draft:cap", hddCapKeys, d.capacityPresets)...)
		r = append(r, prRows("draft:cap", ssdCapKeys, d.capacityPresets)...)
		r = append(r, nav)
		return g(r...)
	case "price":
		r := [][]tele.InlineButton{{ib("Aucune limite", "draft:price:none")}}
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
						c = append(c, ib(l, "draft:price:"+k))
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
		return g(r...)
	case "categories":
		return g(append(optRows(catPairs, d.driveCategories, "draft:toggle:category"), nav)...)
	case "interfaces":
		return g(append(optRows(ifacePairs, d.interfaces, "draft:toggle:interface"), nav)...)
	case "brand":
		return g(append(optRows(brandPairs, d.brands, "draft:toggle:brand"), nav)...)
	case "recording":
		return g(append(optRows(recordingPairs, d.recordingMethods, "draft:toggle:recording"), nav)...)
	default:
		return g([]tele.InlineButton{ib("Creer", "draft:create")}, []tele.InlineButton{ib("Precedent", "draft:prev"), ib("Annuler", "draft:cancel")}, []tele.InlineButton{ib("Accueil", "menu:home")})
	}
}
func optRows(ps [][2]string, sel []string, px string) [][]tele.InlineButton {
	var r [][]tele.InlineButton
	var c []tele.InlineButton
	for _, p := range ps {
		c = append(c, ib(lTgl(p[0], p[1], sel), px+":"+p[1]))
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
