// Package i18n provides a small key-based translation catalog used by the
// web admin. It is intentionally minimal: no formatting helpers, plurals, or fallback chains
// beyond "key missing -> return the key itself". The catalog lives in
// memory; if a deployment ever needs more locales, embed a catalog file
// per language and load it on startup.
//
// The locale is resolved per call from a small set of context sources:
//   - the explicit value passed to T / MustT
//   - the *http.Request cookie "lang" for the web layer
//
// Tests can pin a locale by setting a global default with SetDefault.
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
		// Web — login
		"web.login.title":      "Connexion DiskCount",
		"web.login.password":   "Mot de passe",
		"web.login.submit":     "Se connecter",
		"web.login.error":      "Mot de passe invalide.",
		"web.login.no_pwd":     "Mot de passe admin non configure (definir WEB_ADMIN_PASSWORD).",
		"web.login.intro":      "Saisissez le mot de passe administrateur pour acceder au tableau de bord.",
		"web.login.restricted": "Acces restreint",

		// Web — nav
		"web.nav.dashboard":    "Tableau de bord",
		"web.nav.quality":      "Qualite",
		"web.nav.alerts":       "Alertes",
		"web.nav.create_alert": "Creer une alerte",
		"web.nav.products":     "Produits",
		"web.nav.sites":        "Sites",
		"web.nav.logs":         "Logs",
		"web.nav.drops":        "Baisses de prix",
		"web.nav.discord":      "Discord",
		"web.nav.metrics":      "Metriques",
		"web.nav.config":       "Configuration",
		"web.nav.logout":       "Deconnexion",
		"web.lang.label":       "Langue",
		"web.lang.fr":          "Francais",
		"web.lang.en":          "English",

		// Web — common
		"web.common.empty":        "Aucune donnee.",
		"web.common.empty_alert":  "Aucune alerte.",
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

		// Web — products filters
		"web.products.title":      "Produits",
		"web.products.filter_src": "Source",
		"web.products.all":        "Toutes",
	},
	EN: {
		"web.login.title":      "DiskCount login",
		"web.login.password":   "Password",
		"web.login.submit":     "Sign in",
		"web.login.error":      "Invalid password.",
		"web.login.no_pwd":     "Admin password not configured (set WEB_ADMIN_PASSWORD).",
		"web.login.intro":      "Enter the admin password to access the dashboard.",
		"web.login.restricted": "Restricted access",

		"web.nav.dashboard":    "Dashboard",
		"web.nav.quality":      "Quality",
		"web.nav.alerts":       "Alerts",
		"web.nav.create_alert": "Create alert",
		"web.nav.products":     "Products",
		"web.nav.sites":        "Sites",
		"web.nav.logs":         "Logs",
		"web.nav.drops":        "Price drops",
		"web.nav.discord":      "Discord",
		"web.nav.metrics":      "Metrics",
		"web.nav.config":       "Configuration",
		"web.nav.logout":       "Sign out",
		"web.lang.label":       "Language",
		"web.lang.fr":          "French",
		"web.lang.en":          "English",

		"web.common.empty":        "No data.",
		"web.common.empty_alert":  "No alerts.",
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

		"web.config.title":        "Configuration",
		"web.metrics.title":       "Health & metrics",
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

// SetDefault changes the fallback locale. Useful in tests; the web layer
// resolves the active locale from request state on every call.
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
