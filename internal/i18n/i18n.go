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

		// Web — theme
		"web.theme.light": "Clair",
		"web.theme.dark":  "Sombre",
		"web.theme.auto":  "Auto",
		"web.theme.label": "Thème",

		// Web — accessibility
		"web.a11y.skip_to_content":    "Aller au contenu principal",
		"web.a11y.opens_in_new_tab":   "(s'ouvre dans un nouvel onglet)",

		// Web — topbar
		"web.topbar.discord_ok":  "configure",
		"web.topbar.discord_off": "non configure",
		"web.topbar.sources":     "sources",

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
		"web.common.save":         "Sauvegarder",
		"web.common.cancel":       "Annuler",
		"web.common.error_prefix": "Erreur",
		"web.common.configured":   "Configure",
		"web.common.optional":     "Optionnel",
		"web.common.all":          "Tous",
		"web.common.reset":        "Reinitialiser",
		"web.common.filter":       "Filtrer",
		"web.common.apply":        "Appliquer",
		"web.common.view_offer":   "Voir l'offre",
		"web.common.unavailable":  "Indisponible",
		"web.common.available":    "Disponible",
		"web.common.sellers":      "vendeurs",

		"web.condition.new":     "Neuf",
		"web.condition.used":    "Occasion",
		"web.condition.unknown": "Etat inconnu",

		// Web — dashboard
		"web.dashboard.active_alerts":       "Alertes actives",
		"web.dashboard.inactive_alerts":     "Alertes inactives",
		"web.dashboard.sources_active":      "Sources actives",
		"web.dashboard.warnings_title":      "Sources en alerte",
		"web.dashboard.col_source":          "Source",
		"web.dashboard.col_streak":          "Scans vides consecutifs",
		"web.dashboard.col_message":         "Message",
		"web.dashboard.products":            "Produits",
		"web.dashboard.observations":        "Observations",
		"web.dashboard.notifications":       "Notifications",
		"web.dashboard.discord":             "Discord",
		"web.dashboard.sources":             "Sources",
		"web.dashboard.rejected":            "Rejets donnees",
		"web.dashboard.view_products":       "Voir les produits",
		"web.dashboard.sites_state":         "Etat des sites",
		"web.dashboard.price_drops":         "Baisses de prix",
		"web.dashboard.create_alert":        "Creer une alerte",
		"web.dashboard.market_index":        "Indice du marche",
		"web.dashboard.europe":              "Comparaison europeenne",
		"web.dashboard.tech_metrics":        "Metriques techniques",
		"web.dashboard.active_sources":      "Sources actives",
		"web.dashboard.recent_events":       "Derniers evenements",
		"web.dashboard.web_started":         "Demarrage Web",
		"web.dashboard.last_observation":    "Derniere observation",
		"web.dashboard.last_notification":   "Derniere notification",
		"web.dashboard.last_scan":           "Dernier scan",
		"web.dashboard.next_scan":           "Prochain scan",
		"web.dashboard.last_triggered_alerts": "Dernieres alertes declenchees",
		"web.dashboard.no_triggered":        "Aucune alerte declenchee.",
		"web.dashboard.col_date":            "Date",
		"web.dashboard.col_alert":           "Alerte",
		"web.dashboard.col_product":         "Produit",
		"web.dashboard.col_price":           "Prix",
		"web.dashboard.col_eur_tb":          "EUR/To",
		"web.dashboard.col_reason":          "Raison",
		"web.dashboard.col_offer":           "Offre",

		// Web — products
		"web.products.title":          "Produits",
		"web.products.filter_src":     "Source",
		"web.products.all":            "Toutes",
		"web.products.hero_title":     "Le meilleur du stockage, au bon prix",
		"web.products.hero_sub":       "HDD, SSD et NVMe classes par cout reel au teraoctet.",
		"web.products.count_suffix":   "produits",
		"web.products.filters":        "Filtres",
		"web.products.search":         "Rechercher",
		"web.products.merchant":       "Marchand",
		"web.products.media":          "Support",
		"web.products.condition":      "Etat",
		"web.products.availability":   "Disponibilite",
		"web.products.brand":          "Marque",
		"web.products.form_factor":    "Format",
		"web.products.interface":      "Interface",
		"web.products.recording":      "Enregistrement",
		"web.products.capacity":       "Capacite",
		"web.products.max_price":      "Prix maximum",
		"web.products.sort":           "Trier par",
		"web.products.sort_eur_tb":    "Prix/To",
		"web.products.sort_price":     "Prix",
		"web.products.sort_freshness": "Fraicheur",
		"web.products.sort_sellers":   "Vendeurs",
		"web.products.ungrouped":      "Offres non referencees",
		"web.products.show_products":  "Afficher les produits",
		"web.products.referenced":     "Produits references",
		"web.products.freshness_hint": "Prix et fraicheur verifies lors du dernier scan.",
		"web.products.no_match":       "Aucun produit ne correspond aux filtres.",
		"web.products.mobile_filters": "Filtres",
		"web.products.hdd_ssd":        "HDD et SSD",
		"web.products.new":            "Neuf",
		"web.products.used":           "Occasion",

		// Web — product detail
		"web.product.back":               "Retour aux produits",
		"web.product.create_price_alert": "Creer une alerte de prix",
		"web.product.view_merchant":      "Voir chez le marchand",
		"web.product.price_compare":      "Comparaison des prix",
		"web.product.specs":              "Caracteristiques",
		"web.product.history":            "Historique de prix",
		"web.product.min_eur_tb":         "Minimum EUR/To",
		"web.product.avg_eur_tb":         "Moyenne EUR/To",
		"web.product.max_eur_tb":         "Maximum EUR/To",
		"web.product.not_found":          "Produit introuvable.",

		// Web — alerts
		"web.alerts.title":           "Alertes",
		"web.alerts.existing":        "Alertes existantes",
		"web.alerts.col_name":        "Nom",
		"web.alerts.col_owner":       "Proprietaire",
		"web.alerts.col_state":       "Etat",
		"web.alerts.col_caps":        "Capacites",
		"web.alerts.col_media":       "Media",
		"web.alerts.col_max_price":   "Prix max",
		"web.alerts.col_actions":     "Actions",
		"web.alerts.create_title":    "Creer une alerte",
		"web.alerts.create_hint":     "Tous les criteres sont geres ici. Discord reste optionnel pour la diffusion.",
		"web.alerts.name":            "Nom",
		"web.alerts.max_price":       "Prix max EUR/To",
		"web.alerts.min_drop":        "Baisse minimale %",
		"web.alerts.cooldown":        "Delai entre alertes (h)",
		"web.alerts.keywords":        "Mots inclus",
		"web.alerts.exclude":         "Mots exclus",
		"web.alerts.discord_relay":   "Diffuser cette alerte sur Discord",
		"web.alerts.media":           "Support",
		"web.alerts.condition":       "Etat",
		"web.alerts.capacity":        "Capacite",
		"web.alerts.sources":         "Sources",
		"web.alerts.brands":          "Marques",
		"web.alerts.interfaces":      "Interfaces",
		"web.alerts.recording":       "Enregistrement",
		"web.alerts.categories":      "Categories",
		"web.alerts.submit":          "Creer l'alerte",
		"web.alerts.pause":           "Pause",
		"web.alerts.resume":          "Reprendre",
		"web.alerts.delete":          "Supprimer",
		"web.alerts.confirm_delete":  "Supprimer cette alerte ?",
		"web.alerts.active":          "active",
		"web.alerts.inactive":        "inactive",
		"web.alerts.discord_yes":     "coche",
		"web.alerts.discord_no":      "non",

		// Web — config
		"web.config.title":              "Configuration",
		"web.config.saved":              "Configuration sauvegardee.",
		"web.config.restart_note":       "Certains parametres necessitent un redemarrage du service.",
		"web.config.params":             "Parametres applicatifs",
		"web.config.replace":            "Remplacer",
		"web.config.restart_badge":      "Redemarrage",
		"web.config.section_essential":  "Essentiel",
		"web.config.section_merchants":  "Marchands",
		"web.config.section_apis":       "APIs",
		"web.config.section_advanced":   "Avance",
		"web.config.merchants_hint":     "Gerer les marchands actifs sur la page Sites.",
		"web.config.show_advanced_urls": "Afficher les URLs avancees",
		"web.config.save":               "Sauvegarder",

		// Web — sites
		"web.sites.title":                 "Statistiques des sites",
		"web.sites.subtitle":              "Etat du dernier scan et qualite des offres par fournisseur",
		"web.sites.site":                  "Site",
		"web.sites.status":                "Statut",
		"web.sites.offers":                "Offres",
		"web.sites.products":              "Produits",
		"web.sites.observations":          "Observations",
		"web.sites.rejects":               "Rejets",
		"web.sites.last_refresh":          "Dernier refresh",
		"web.sites.duration":              "Duree",
		"web.sites.breaker":               "Breaker",
		"web.sites.median":                "Mediane EUR/To",
		"web.sites.none":                  "Aucun fournisseur configure.",
		"web.sites.merchants_toggle_title": "Marchands actifs",
		"web.sites.merchants_toggle_hint":  "Activez ou desactivez les sources de scan.",
		"web.sites.byparr_status":         "Byparr",
		"web.sites.byparr_ok":             "Byparr actif",
		"web.sites.byparr_off":            "Byparr inactif",
		"web.sites.save_merchants":        "Sauvegarder les marchands",
		"web.sites.enabled":               "Active",

		// Web — logs
		"web.logs.title":    "Journal du dernier scan",
		"web.logs.subtitle": "Les erreurs sont rouges, les avertissements orange.",
		"web.logs.none":     "Aucun scan enregistre.",

		// Web — price drops
		"web.drops.title":         "Baisses de prix",
		"web.drops.subtitle":      "Les offres dont le prix actuel vient reellement de diminuer.",
		"web.drops.count_suffix":  "baisses",
		"web.drops.period_days":   "Periode (jours)",
		"web.drops.min_drop":      "Baisse minimale %",
		"web.drops.load_error":    "Impossible de charger les baisses",
		"web.drops.empty":         "Aucune baisse trouvee avec ces criteres.",

		// Web — market index
		"web.market.title":        "Indice quotidien du marche",
		"web.market.subtitle":     "Mediane observee du prix par teraoctet, regroupee par tranche de capacite.",
		"web.market.load_error":   "Impossible de charger l'indice",
		"web.market.col_day":      "Jour UTC",
		"web.market.col_capacity": "Capacite",
		"web.market.col_median":   "Mediane EUR/To",
		"web.market.col_samples":  "Observations",
		"web.market.empty":        "Aucune observation qualifiee sur cette periode.",
		"web.market.period_7":     "7 jours",
		"web.market.period_30":    "30 jours",
		"web.market.period_90":    "90 jours",

		// Web — europe
		"web.europe.title":             "Comparaison europeenne",
		"web.europe.subtitle":          "Prix observes par boutique nationale, classes au cout par teraoctet.",
		"web.europe.disclaimer":        "Les frais de port et les restrictions de livraison ne sont pas inclus. Verifiez le total chez le marchand.",
		"web.europe.load_error":        "Impossible de charger la comparaison",
		"web.europe.all_europe":        "Toute l'Europe",
		"web.europe.compare_countries": "Comparer les pays",
		"web.europe.offers_count":      "offres",
		"web.europe.from_price":        "Des",
		"web.europe.empty":             "Aucune offre fiable pour ce pays.",

		// Web — quality
		"web.quality.title":                  "Qualite par source",
		"web.quality.top_rejects":            "Top raisons de rejet",
		"web.quality.col_missing_title":      "Titre manquant",
		"web.quality.col_missing_media":      "Media manquant",
		"web.quality.col_missing_category":   "Categorie manquante",
		"web.quality.col_missing_interfaces": "Interfaces manquantes",
		"web.quality.col_min":                "EUR/To min",
		"web.quality.col_max":                "Max",
		"web.quality.col_reason":             "Raison",
		"web.quality.col_count":              "Nombre",

		// Web — metrics
		"web.metrics.title":          "Sante & metriques",
		"web.metrics.last_scan":      "Dernier scan",
		"web.metrics.fetched":        "Fetched",
		"web.metrics.accepted":       "Accepted",
		"web.metrics.rejected":       "Rejected",
		"web.metrics.matched":        "Matched",
		"web.metrics.notified":       "Notified",
		"web.metrics.errors":         "Errors",
		"web.metrics.breaker_skips":  "Breaker skips",
		"web.metrics.source_health":  "Sante des sources",
		"web.metrics.col_state":      "Etat",
		"web.metrics.col_action":     "Action",
		"web.metrics.reset":          "Reinitialiser",
		"web.metrics.per_source":     "Metriques par source (dernier scan)",
		"web.metrics.col_deals":      "Deals",
		"web.metrics.col_error":      "Erreur",
		"web.metrics.no_scan":        "Aucun scan.",

		// Web — discord
		"web.discord.title":             "Bot Discord de diffusion",
		"web.discord.subtitle":          "Le bot publie uniquement les alertes creees dans DiskCount dont la case Discord est cochee.",
		"web.discord.saved":             "Configuration Discord sauvegardee et appliquee.",
		"web.discord.tested":            "Message de test Discord envoye.",
		"web.discord.pending_note":      "Integration prete mais laissee en attente. Configurez le bot seulement apres validation complete du suivi des produits et des alertes.",
		"web.discord.bot_title":         "Bot Discord de diffusion",
		"web.discord.bot_hint":          "Le bot publie uniquement les alertes dont la case Discord est cochee.",
		"web.discord.channel_id":        "Identifiant du salon",
		"web.discord.bot_token":         "Token du bot",
		"web.discord.replace_token":     "Remplacer le token enregistre",
		"web.discord.permissions_note":  "Permissions minimales du bot dans ce salon : Voir le salon et Envoyer des messages.",
		"web.discord.save":              "Sauvegarder Discord",
		"web.discord.test":              "Envoyer un message de test",
		"web.discord.test_unavailable":  "Le test sera disponible apres configuration du token et du salon.",
		"web.discord.error":             "Erreur",
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

		"web.theme.light": "Light",
		"web.theme.dark":  "Dark",
		"web.theme.auto":  "Auto",
		"web.theme.label": "Theme",

		"web.a11y.skip_to_content":  "Skip to main content",
		"web.a11y.opens_in_new_tab": "(opens in new tab)",

		"web.topbar.discord_ok":  "configured",
		"web.topbar.discord_off": "not configured",
		"web.topbar.sources":     "sources",

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
		"web.common.save":         "Save",
		"web.common.cancel":       "Cancel",
		"web.common.error_prefix": "Error",
		"web.common.configured":   "Configured",
		"web.common.optional":     "Optional",
		"web.common.all":          "All",
		"web.common.reset":        "Reset",
		"web.common.filter":       "Filter",
		"web.common.apply":        "Apply",
		"web.common.view_offer":   "View offer",
		"web.common.unavailable":  "Unavailable",
		"web.common.available":    "Available",
		"web.common.sellers":      "sellers",

		"web.condition.new":     "New",
		"web.condition.used":    "Used",
		"web.condition.unknown": "Unknown condition",

		"web.dashboard.active_alerts":       "Active alerts",
		"web.dashboard.inactive_alerts":     "Inactive alerts",
		"web.dashboard.sources_active":      "Active sources",
		"web.dashboard.warnings_title":      "Sources in alert",
		"web.dashboard.col_source":          "Source",
		"web.dashboard.col_streak":          "Consecutive empty scans",
		"web.dashboard.col_message":         "Message",
		"web.dashboard.products":            "Products",
		"web.dashboard.observations":        "Observations",
		"web.dashboard.notifications":       "Notifications",
		"web.dashboard.discord":             "Discord",
		"web.dashboard.sources":             "Sources",
		"web.dashboard.rejected":            "Rejected deals",
		"web.dashboard.view_products":       "View products",
		"web.dashboard.sites_state":         "Sites status",
		"web.dashboard.price_drops":         "Price drops",
		"web.dashboard.create_alert":        "Create alert",
		"web.dashboard.market_index":        "Market index",
		"web.dashboard.europe":              "European comparison",
		"web.dashboard.tech_metrics":        "Technical metrics",
		"web.dashboard.active_sources":      "Active sources",
		"web.dashboard.recent_events":       "Recent events",
		"web.dashboard.web_started":         "Web started",
		"web.dashboard.last_observation":    "Last observation",
		"web.dashboard.last_notification":   "Last notification",
		"web.dashboard.last_scan":           "Last scan",
		"web.dashboard.next_scan":           "Next scan",
		"web.dashboard.last_triggered_alerts": "Recently triggered alerts",
		"web.dashboard.no_triggered":        "No triggered alerts.",
		"web.dashboard.col_date":            "Date",
		"web.dashboard.col_alert":           "Alert",
		"web.dashboard.col_product":         "Product",
		"web.dashboard.col_price":           "Price",
		"web.dashboard.col_eur_tb":          "EUR/TB",
		"web.dashboard.col_reason":          "Reason",
		"web.dashboard.col_offer":           "Offer",

		"web.products.title":          "Products",
		"web.products.filter_src":     "Source",
		"web.products.all":            "All",
		"web.products.hero_title":     "The best storage, at the right price",
		"web.products.hero_sub":       "HDD, SSD and NVMe ranked by real cost per terabyte.",
		"web.products.count_suffix":   "products",
		"web.products.filters":        "Filters",
		"web.products.search":         "Search",
		"web.products.merchant":       "Merchant",
		"web.products.media":          "Media",
		"web.products.condition":      "Condition",
		"web.products.availability":   "Availability",
		"web.products.brand":          "Brand",
		"web.products.form_factor":    "Form factor",
		"web.products.interface":      "Interface",
		"web.products.recording":      "Recording",
		"web.products.capacity":       "Capacity",
		"web.products.max_price":      "Max price",
		"web.products.sort":           "Sort by",
		"web.products.sort_eur_tb":    "Price/TB",
		"web.products.sort_price":     "Price",
		"web.products.sort_freshness": "Freshness",
		"web.products.sort_sellers":   "Sellers",
		"web.products.ungrouped":      "Ungrouped offers",
		"web.products.show_products":  "Show products",
		"web.products.referenced":     "Referenced products",
		"web.products.freshness_hint": "Prices and freshness verified on the last scan.",
		"web.products.no_match":       "No product matches the filters.",
		"web.products.mobile_filters": "Filters",
		"web.products.hdd_ssd":        "HDD and SSD",
		"web.products.new":            "New",
		"web.products.used":           "Used",

		"web.product.back":               "Back to products",
		"web.product.create_price_alert": "Create price alert",
		"web.product.view_merchant":      "View at merchant",
		"web.product.price_compare":      "Price comparison",
		"web.product.specs":              "Specifications",
		"web.product.history":            "Price history",
		"web.product.min_eur_tb":         "Minimum EUR/TB",
		"web.product.avg_eur_tb":         "Average EUR/TB",
		"web.product.max_eur_tb":         "Maximum EUR/TB",
		"web.product.not_found":          "Product not found.",

		"web.alerts.title":           "Alerts",
		"web.alerts.existing":        "Existing alerts",
		"web.alerts.col_name":        "Name",
		"web.alerts.col_owner":       "Owner",
		"web.alerts.col_state":       "State",
		"web.alerts.col_caps":        "Capacities",
		"web.alerts.col_media":       "Media",
		"web.alerts.col_max_price":   "Max price",
		"web.alerts.col_actions":     "Actions",
		"web.alerts.create_title":    "Create alert",
		"web.alerts.create_hint":     "All criteria are managed here. Discord remains optional for delivery.",
		"web.alerts.name":            "Name",
		"web.alerts.max_price":       "Max price EUR/TB",
		"web.alerts.min_drop":        "Minimum drop %",
		"web.alerts.cooldown":        "Cooldown between alerts (h)",
		"web.alerts.keywords":        "Include keywords",
		"web.alerts.exclude":         "Exclude keywords",
		"web.alerts.discord_relay":   "Relay this alert to Discord",
		"web.alerts.media":           "Media",
		"web.alerts.condition":       "Condition",
		"web.alerts.capacity":        "Capacity",
		"web.alerts.sources":         "Sources",
		"web.alerts.brands":          "Brands",
		"web.alerts.interfaces":      "Interfaces",
		"web.alerts.recording":       "Recording",
		"web.alerts.categories":      "Categories",
		"web.alerts.submit":          "Create alert",
		"web.alerts.pause":           "Pause",
		"web.alerts.resume":          "Resume",
		"web.alerts.delete":          "Delete",
		"web.alerts.confirm_delete":  "Delete this alert?",
		"web.alerts.active":          "active",
		"web.alerts.inactive":        "inactive",
		"web.alerts.discord_yes":     "yes",
		"web.alerts.discord_no":      "no",

		"web.config.title":              "Configuration",
		"web.config.saved":              "Configuration saved.",
		"web.config.restart_note":       "Some settings require a service restart.",
		"web.config.params":             "Application settings",
		"web.config.replace":            "Replace",
		"web.config.restart_badge":      "Restart",
		"web.config.section_essential":  "Essential",
		"web.config.section_merchants":  "Merchants",
		"web.config.section_apis":       "APIs",
		"web.config.section_advanced":   "Advanced",
		"web.config.merchants_hint":     "Manage active merchants on the Sites page.",
		"web.config.show_advanced_urls": "Show advanced URLs",
		"web.config.save":               "Save",

		"web.sites.title":                  "Site statistics",
		"web.sites.subtitle":               "Last scan status and offer quality per merchant",
		"web.sites.site":                   "Site",
		"web.sites.status":                 "Status",
		"web.sites.offers":                 "Offers",
		"web.sites.products":               "Products",
		"web.sites.observations":           "Observations",
		"web.sites.rejects":                "Rejections",
		"web.sites.last_refresh":           "Last refresh",
		"web.sites.duration":               "Duration",
		"web.sites.breaker":                "Breaker",
		"web.sites.median":                 "Median EUR/TB",
		"web.sites.none":                   "No merchant configured.",
		"web.sites.merchants_toggle_title": "Active merchants",
		"web.sites.merchants_toggle_hint":  "Enable or disable scan sources.",
		"web.sites.byparr_status":          "Byparr",
		"web.sites.byparr_ok":              "Byparr active",
		"web.sites.byparr_off":             "Byparr inactive",
		"web.sites.save_merchants":         "Save merchants",
		"web.sites.enabled":                "Enabled",

		"web.logs.title":    "Last scan log",
		"web.logs.subtitle": "Errors are red, warnings are orange.",
		"web.logs.none":     "No scan recorded.",

		"web.drops.title":        "Price drops",
		"web.drops.subtitle":     "Offers whose current price has actually decreased.",
		"web.drops.count_suffix": "drops",
		"web.drops.period_days":  "Period (days)",
		"web.drops.min_drop":     "Minimum drop %",
		"web.drops.load_error":   "Unable to load price drops",
		"web.drops.empty":        "No drops found with these criteria.",

		"web.market.title":        "Daily market index",
		"web.market.subtitle":     "Observed median price per terabyte, grouped by capacity band.",
		"web.market.load_error":   "Unable to load market index",
		"web.market.col_day":      "UTC day",
		"web.market.col_capacity": "Capacity",
		"web.market.col_median":   "Median EUR/TB",
		"web.market.col_samples":  "Observations",
		"web.market.empty":        "No qualified observations in this period.",
		"web.market.period_7":     "7 days",
		"web.market.period_30":    "30 days",
		"web.market.period_90":    "90 days",

		"web.europe.title":             "European comparison",
		"web.europe.subtitle":          "Observed prices by national store, ranked by cost per terabyte.",
		"web.europe.disclaimer":        "Shipping fees and delivery restrictions are not included. Check the total at the merchant.",
		"web.europe.load_error":        "Unable to load comparison",
		"web.europe.all_europe":        "All of Europe",
		"web.europe.compare_countries": "Compare countries",
		"web.europe.offers_count":      "offers",
		"web.europe.from_price":        "From",
		"web.europe.empty":             "No reliable offer for this country.",

		"web.quality.title":                  "Quality by source",
		"web.quality.top_rejects":            "Top rejection reasons",
		"web.quality.col_missing_title":      "Missing title",
		"web.quality.col_missing_media":      "Missing media",
		"web.quality.col_missing_category":   "Missing category",
		"web.quality.col_missing_interfaces": "Missing interfaces",
		"web.quality.col_min":                "Min EUR/TB",
		"web.quality.col_max":                "Max",
		"web.quality.col_reason":             "Reason",
		"web.quality.col_count":              "Count",

		"web.metrics.title":         "Health & metrics",
		"web.metrics.last_scan":     "Last scan",
		"web.metrics.fetched":       "Fetched",
		"web.metrics.accepted":      "Accepted",
		"web.metrics.rejected":      "Rejected",
		"web.metrics.matched":       "Matched",
		"web.metrics.notified":      "Notified",
		"web.metrics.errors":        "Errors",
		"web.metrics.breaker_skips": "Breaker skips",
		"web.metrics.source_health": "Source health",
		"web.metrics.col_state":     "State",
		"web.metrics.col_action":    "Action",
		"web.metrics.reset":         "Reset",
		"web.metrics.per_source":    "Per-source metrics (last scan)",
		"web.metrics.col_deals":     "Deals",
		"web.metrics.col_error":     "Error",
		"web.metrics.no_scan":       "No scan.",

		"web.discord.title":            "Discord relay bot",
		"web.discord.subtitle":         "The bot only publishes DiskCount alerts with Discord enabled.",
		"web.discord.saved":            "Discord configuration saved and applied.",
		"web.discord.tested":           "Discord test message sent.",
		"web.discord.pending_note":     "Integration is ready but left on hold. Configure the bot only after product tracking and alerts are fully validated.",
		"web.discord.bot_title":        "Discord relay bot",
		"web.discord.bot_hint":         "The bot only publishes alerts with Discord enabled.",
		"web.discord.channel_id":       "Channel ID",
		"web.discord.bot_token":        "Bot token",
		"web.discord.replace_token":    "Replace stored token",
		"web.discord.permissions_note": "Minimum bot permissions in this channel: View channel and Send messages.",
		"web.discord.save":             "Save Discord",
		"web.discord.test":             "Send test message",
		"web.discord.test_unavailable": "Test will be available after configuring the token and channel.",
		"web.discord.error":            "Error",
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
