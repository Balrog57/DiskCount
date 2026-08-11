// Package i18n provides a small key-based translation catalog used by the
// bot, the web admin, and the notifier. It is intentionally minimal: the
// rest of the codebase calls T("bot.welcome") and gets a string in the
// current locale. No formatting helpers, no plurals, no fallback chains
// beyond "key missing -> return the key itself". The catalog lives in
// memory; if a deployment ever needs more locales, embed a catalog file
// per language and load it on startup.
//
// The locale is resolved per call from a small set of context sources:
//   - the explicit value passed to T / MustT
//   - the *http.Request cookie "lang" for the web layer
//   - the *telebot.Context message sender language for the bot
//
// Tests can pin a locale by calling WithLocale or by setting a global
// default with SetDefault.
package i18n

import (
	"sync"
)

// Locale identifies a translation language. New locales only require
// adding an entry to the catalogs map below.
type Locale string

const (
	FR Locale = "fr"
	EN Locale = "en"
)

// Default is the fallback locale used when no other source resolves one.
var Default Locale = FR

// catalogs holds the per-locale key -> translation map. Keeping both
// locales in the same struct guarantees the keys stay in sync; missing
// translations fall through to the other locale instead of returning the
// raw key, which keeps the UI usable even mid-rollout.
var catalogs = map[Locale]map[string]string{
	FR: {
		// Bot — global
		"bot.welcome":          "Bienvenue sur DiskCount. Que veux-tu faire ?",
		"bot.help":             "Aide",
		"bot.cancel":           "Annuler",
		"bot.back":             "Retour",
		"bot.unknown":          "Commande inconnue. Tapez /help.",
		"bot.no_alert":         "Aucune alerte. Utilise /create.",
		"bot.no_alert_edit":    "Aucune alerte.",
		"bot.open_offer":       "Ouvrir l'offre",
		"bot.connected_intro":  "Connexions\n\nLes connexions filtrent SATA, SAS, NVMe ou USB quand l'information est connue.",
		"bot.no_prices":        "Prix actuels\n\nAucun prix connu pour le moment. Les prix seront disponibles apres un scan automatique.",
		"bot.filters_help":     "Filtres",
		"bot.commands_help":    "Commandes",
		"bot.prices_help":      "Prix actuels",
		"bot.capacity_help":    "Capacites",
		"bot.categories_help":  "Categories",
		"bot.connections_help": "Connexions",
		"bot.help_create":      "Creer",
		"bot.help_alerts":      "Alertes",
		"bot.create":           "Creer",
		"bot.alerts":           "Alertes",
		"bot.sources":          "Sources",
		"bot.any":              "Toutes",
		"bot.none_limit":       "Aucune limite",
		"bot.alerts_updated":   "Alertes mises a jour.",
		"bot.config_saved":     "Configuration sauvegardee.",
		"bot.saved":            "Alertes mises a jour.",

		// Bot — wizard step labels
		"bot.step.media":      "Type de disque",
		"bot.step.condition":  "Etat produit",
		"bot.step.capacity":   "Capacite",
		"bot.step.price":      "Prix",
		"bot.step.categories": "Categories",
		"bot.step.interfaces": "Connexions",
		"bot.step.brand":      "Marque",
		"bot.step.recording":  "Enregistrement",
		"bot.step.confirm":    "Recapitulatif",

		// Notifier
		"notifier.condition_new":  "Neuf",
		"notifier.condition_used": "Occasion",
		"notifier.media_hdd":      "HDD",
		"notifier.media_ssd":      "SSD",
		"notifier.source_health":  "Source sante",

		// Web — login
		"web.login.title":      "Connexion DiskCount",
		"web.login.password":   "Mot de passe",
		"web.login.submit":     "Se connecter",
		"web.login.error":      "Mot de passe invalide.",
		"web.login.no_pwd":     "Mot de passe admin non configure (definir WEB_ADMIN_PASSWORD).",
		"web.login.intro":      "Saisissez le mot de passe administrateur pour acceder au tableau de bord.",
		"web.login.restricted": "Acces restreint",
		"web.theme.light":      "Thème clair",
		"web.theme.dark":       "Thème sombre",
		"web.theme.auto":       "Thème automatique",

		// Web — nav
		"web.nav.dashboard": "Tableau de bord",
		"web.nav.quality":   "Qualite",
		"web.nav.alerts":    "Alertes",
		"web.nav.products":  "Produits",
		"web.nav.metrics":   "Metriques",
		"web.nav.config":    "Configuration",
		"web.nav.users":     "Utilisateurs",
		"web.nav.logout":    "Deconnexion",
		"web.lang.label":    "Langue",
		"web.lang.fr":       "Francais",
		"web.lang.en":       "English",

		// Web — common
		"web.common.empty":        "Aucune donnee.",
		"web.common.empty_alert":  "Aucune alerte.",
		"web.common.empty_user":   "Aucun utilisateur.",
		"web.common.no_scan":      "Aucun scan n'a encore eu lieu.",
		"web.common.no_metrics":   "Aucune metrique.",
		"web.common.no_breaker":   "Aucun breaker.",
		"web.common.no_source":    "Aucune source active",
		"web.common.no_reject":    "Aucun rejet.",
		"web.common.no_obs":       "Aucune observation sur cette periode.",
		"web.common.no_match":     "Aucun produit ne correspond aux filtres.",
		"web.common.saved":        "Alertes mises a jour.",
		"web.common.config_saved": "Configuration sauvegardee.",

		// Web — dashboard
		"web.dashboard.active_alerts":   "Alertes actives",
		"web.dashboard.inactive_alerts": "Alertes inactives",
		"web.dashboard.sources_active":  "Sources actives",
		"web.dashboard.warnings_title":  "Sources en alerte",
		"web.dashboard.col_source":      "Source",
		"web.dashboard.col_streak":      "Scans vides consecutifs",
		"web.dashboard.col_message":     "Message",

		// Web — alerts page
		"web.alerts.title":         "Alertes",
		"web.alerts.existing":      "Alertes existantes",
		"web.alerts.col_name":      "Nom",
		"web.alerts.col_owner":     "Proprietaire",
		"web.alerts.col_state":     "Etat",
		"web.alerts.col_caps":      "Capacites",
		"web.alerts.col_media":     "Media",
		"web.alerts.col_max_price": "Prix max",
		"web.alerts.col_actions":   "Actions",

		// Web — config page
		"web.config.title": "Configuration",

		// Web — metrics page
		"web.metrics.title": "Sante & metriques",

		// Web — users page
		"web.users.title":     "Utilisateurs",
		"web.users.col_label": "Libelle",
		"web.users.col_tgid":  "Telegram User ID",
		"web.users.col_state": "Etat",
		"web.users.active":    "actif",
		"web.users.disabled":  "desactive",
		"web.users.disable":   "Desactiver",
		"web.users.enable":    "Reactiver",

		// Web — products filters
		"web.products.title":      "Produits",
		"web.products.filter_src": "Source",
		"web.products.all":        "Toutes",
	},
	EN: {
		"bot.welcome":          "Welcome to DiskCount. What do you want to do?",
		"bot.help":             "Help",
		"bot.cancel":           "Cancel",
		"bot.back":             "Back",
		"bot.unknown":          "Unknown command. Type /help.",
		"bot.no_alert":         "No alerts yet. Use /create.",
		"bot.no_alert_edit":    "No alerts.",
		"bot.open_offer":       "Open offer",
		"bot.connected_intro":  "Connections\n\nConnections filter SATA, SAS, NVMe or USB when the information is known.",
		"bot.no_prices":        "Latest prices\n\nNo known prices yet. Prices will appear after the first scan.",
		"bot.filters_help":     "Filters",
		"bot.commands_help":    "Commands",
		"bot.prices_help":      "Latest prices",
		"bot.capacity_help":    "Capacities",
		"bot.categories_help":  "Categories",
		"bot.connections_help": "Connections",
		"bot.help_create":      "Create",
		"bot.help_alerts":      "Alerts",
		"bot.create":           "Create",
		"bot.alerts":           "Alerts",
		"bot.sources":          "Sources",
		"bot.any":              "All",
		"bot.none_limit":       "No limit",
		"bot.alerts_updated":   "Alerts updated.",
		"bot.config_saved":     "Configuration saved.",
		"bot.saved":            "Alerts updated.",

		"bot.step.media":      "Drive type",
		"bot.step.condition":  "Condition",
		"bot.step.capacity":   "Capacity",
		"bot.step.price":      "Price",
		"bot.step.categories": "Categories",
		"bot.step.interfaces": "Connections",
		"bot.step.brand":      "Brand",
		"bot.step.recording":  "Recording",
		"bot.step.confirm":    "Summary",

		"notifier.condition_new":  "New",
		"notifier.condition_used": "Used",
		"notifier.media_hdd":      "HDD",
		"notifier.media_ssd":      "SSD",
		"notifier.source_health":  "Source health",

		"web.login.title":      "DiskCount login",
		"web.login.password":   "Password",
		"web.login.submit":     "Sign in",
		"web.login.error":      "Invalid password.",
		"web.login.no_pwd":     "Admin password not configured (set WEB_ADMIN_PASSWORD).",
		"web.login.intro":      "Enter the admin password to access the dashboard.",
		"web.login.restricted": "Restricted access",
		"web.theme.light":      "Light theme",
		"web.theme.dark":       "Dark theme",
		"web.theme.auto":       "Auto theme",

		"web.nav.dashboard": "Dashboard",
		"web.nav.quality":   "Quality",
		"web.nav.alerts":    "Alerts",
		"web.nav.products":  "Products",
		"web.nav.metrics":   "Metrics",
		"web.nav.config":    "Configuration",
		"web.nav.users":     "Users",
		"web.nav.logout":    "Sign out",
		"web.lang.label":    "Language",
		"web.lang.fr":       "French",
		"web.lang.en":       "English",

		"web.common.empty":        "No data.",
		"web.common.empty_alert":  "No alerts.",
		"web.common.empty_user":   "No users.",
		"web.common.no_scan":      "No scan has run yet.",
		"web.common.no_metrics":   "No metrics.",
		"web.common.no_breaker":   "No breakers.",
		"web.common.no_source":    "No active source",
		"web.common.no_reject":    "No rejections.",
		"web.common.no_obs":       "No observation in this period.",
		"web.common.no_match":     "No product matches the filters.",
		"web.common.saved":        "Alerts updated.",
		"web.common.config_saved": "Configuration saved.",

		"web.dashboard.active_alerts":   "Active alerts",
		"web.dashboard.inactive_alerts": "Inactive alerts",
		"web.dashboard.sources_active":  "Active sources",
		"web.dashboard.warnings_title":  "Sources in alert",
		"web.dashboard.col_source":      "Source",
		"web.dashboard.col_streak":      "Consecutive empty scans",
		"web.dashboard.col_message":     "Message",

		"web.alerts.title":         "Alerts",
		"web.alerts.existing":      "Existing alerts",
		"web.alerts.col_name":      "Name",
		"web.alerts.col_owner":     "Owner",
		"web.alerts.col_state":     "State",
		"web.alerts.col_caps":      "Capacities",
		"web.alerts.col_media":     "Media",
		"web.alerts.col_max_price": "Max price",
		"web.alerts.col_actions":   "Actions",

		"web.config.title":    "Configuration",
		"web.metrics.title":   "Health & metrics",
		"web.users.title":     "Users",
		"web.users.col_label": "Label",
		"web.users.col_tgid":  "Telegram User ID",
		"web.users.col_state": "State",
		"web.users.active":    "active",
		"web.users.disabled":  "disabled",
		"web.users.disable":   "Disable",
		"web.users.enable":    "Enable",

		"web.products.title":      "Products",
		"web.products.filter_src": "Source",
		"web.products.all":        "All",
	},
}

// T returns the translation for key in the given locale. If the key is
// missing in that locale we fall back to the default locale, then to the
// key itself, so the UI never crashes on a missing translation.
func T(key string, loc Locale) string {
	if loc == "" {
		loc = Default
	}
	if m, ok := catalogs[loc]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if m, ok := catalogs[Default]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	return key
}

// SetDefault changes the fallback locale. Useful in tests; the bot and web
// layer resolve the active locale from request/chat state on every call.
func SetDefault(loc Locale) {
	mu.Lock()
	defer mu.Unlock()
	Default = loc
}

// ParseLocale normalises a user-supplied language string ("fr", "fr-FR",
// "en-US") into a known Locale. Unknown values fall back to the default.
func ParseLocale(s string) Locale {
	if len(s) >= 2 {
		switch s[:2] {
		case "fr":
			return FR
		case "en":
			return EN
		}
	}
	return Default
}

// KnownLocales returns the list of locales that have a catalog. Used by
// the language switcher in the web admin.
func KnownLocales() []Locale {
	out := make([]Locale, 0, len(catalogs))
	for k := range catalogs {
		out = append(out, k)
	}
	return out
}

// mu guards Default during tests; production code is read-only.
var mu sync.RWMutex
