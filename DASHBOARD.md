# Dashboard de realisation DiskCount

Derniere mise a jour : 2026-04-19

## Statut global

| Lot | Statut | Notes |
| --- | --- | --- |
| Squelette Python | Termine | `pyproject.toml`, package `diskcount`, CLI `python -m diskcount`. |
| Configuration | Termine | Variables d'environnement via `pydantic-settings`, `.env` supporte en local; listes CSV supportees. |
| Stockage SQLite | Termine | Alertes, produits, observations, notifications, subscribers. |
| Collecteur DiskPrices | Termine | Dry-run live verifie : 429 offres parsees, 0 erreur. |
| Collecteur PricePerGig | Termine | API JSON publique `api.pricepergig.com/drives`, filtre `amazon.fr`, pagination 50 lignes. |
| Collecteur PricePerTB | Termine | Parsing du tableau public `https://pricepertb.com/fr`, avec fallback Playwright si HTTP direct echoue. |
| Collecteur Dealabs RSS | Termine | Parsing RSS configure par `DEALABS_RSS_URLS`. |
| Collecteur eBay | Termine | API officielle Browse, active avec credentials eBay. |
| Flux/pages Idealo | Termine v2 | Via `IDEALO_FEED_URLS`; pages publiques via `IDEALO_PAGE_URLS` avec fallback Playwright headless. |
| Flux/pages leDenicheur | Termine v2 | Via `LEDENICHEUR_FEED_URLS`; pages publiques via `LEDENICHEUR_PAGE_URLS` avec fallback Playwright headless. |
| Flux leboncoin | Termine v1 | Via `LEBONCOIN_FEED_URLS`, sans scraping de pages. |
| Connecteur Keepa API | Termine v1 | Optionnel, actif seulement avec `KEEPA_API_KEY` + `KEEPA_ASINS`. |
| Regles de notification | Termine | Seuil EUR/To, remise rolling 30 jours, cooldown, baisse significative. |
| Bot Telegram | Termine | Commandes demandees implementees. |
| Menu commandes Telegram | Termine | `setMyCommands` configure un menu `/` utilisateur et un menu admin pour `TELEGRAM_ADMIN_USER_IDS`. |
| Tuiles commandes Telegram | Termine | `/start`, `/menu` et `/help` affichent une navigation inline par categories; sous-menus avec `Precedent` et `Accueil`. |
| Guide Aide Telegram | Termine | `Aide` contient un guide complet en tuiles pour creation, alertes, capacites, prix, categories, connexions, scan/test, admin, sources backend, commandes et filtres texte. |
| Edition alertes Telegram | Termine | `Mes alertes` ouvre chaque alerte comme tuile; modification cliquable de type, etat, capacites multi-selection, prix, categories, connexions, pause/reprise et suppression confirmee. |
| Filtres DiskPrices avances | Termine | Categories interne/externe/form factor, connectiques SATA/SAS/NVMe/USB, `max_eur_gb` SSD converti en EUR/To. |
| Panel admin Telegram | Termine | `/users`, `/allow <id> <nom>`, `/revoke <id>` avec super-admin env. |
| Alertes par utilisateur | Termine | Chaque utilisateur autorise possede ses alertes et ne peut modifier/supprimer que les siennes. |
| Boutons Telegram | Termine | Les alertes incluent un bouton direct vers l'offre. |
| Assistant de creation d'alerte | Termine | Commande `/create` et bouton `Creer une alerte`; wizard 100% tuiles avec presets HDD/SSD multi-selection, prix, categories, connexions et recapitulatif. |
| CLI kimsufi-like | Termine | `check`, `list`, `scan`, `run`, `init-db`. |
| Deploiement Debian | Termine | Fichiers `deploy/diskcount.service` et `deploy/diskcount.env.example`. |
| Deploiement server | Termine | Service `diskcount` actif sur `<SERVER_IP>`; bot `@DiskCount_bot` en polling. |
| Cadence scanner | Termine | `POLL_INTERVAL_SECONDS=14400` par defaut, soit un scan toutes les 4h. |
| Tests | Termine | 37 tests passent. |
| Documentation | En cours | README, plan projet et dashboard presents. |
| Workflow projet | Actif | Toute evolution doit mettre a jour les `.md` concernes et etre poussee sur le repo prive GitHub. |
| Menage depot | Termine | Artefacts locaux non suivis supprimes; `.gitignore` couvre archives zip, backups et exports temporaires. |
| Acces VPS SSH server | Debloque | Seule l'IP client `<YOUR_CLIENT_IP>` est en ignoreip fail2ban et autorisee UFW sur `<SSH_PORT>/tcp`; fail2ban reste actif pour les autres IPs. |
| Acces VPS SSH server | Debloque | `<YOUR_CLIENT_IP>` est en ignoreip fail2ban et autorisee UFW sur `<SERVER_IP>:<SSH_PORT>`. |
| Acces VPS SSH server | Debloque | `<YOUR_CLIENT_IP>` est en ignoreip fail2ban et autorisee UFW sur `<SERVER_IP>:<SSH_PORT>`. |

## Verification executee

```text
python -m compileall -q diskcount tests
Resultat: OK
```

```text
pytest -q
Resultat: 37 passed in 1.68s
```

```text
python -m diskcount list --min-tb 16 --max-eur-tb 20 --media rotational
Resultat: diskprices=439, pricepergig=200, pricepertb=419, errors=0
Top live liste: 16 To a 18.69 EUR/To, 18 To a 19.25 EUR/To, 22 To a 19.50 EUR/To
```

```text
server systemd
Resultat: diskcount.service active
Bot: @DiskCount_bot
Dernier scan: fetched=435 matched=0 notified=0 errors=0
Migration SQLite: colonne alerts.owner_user_id presente
Menu inline deploye: service actif apres redemarrage, dernier scan fetched=432 matched=0 notified=0 errors=0
Edition alertes avancee deployee: service actif, dernier scan fetched=432 matched=0 notified=0 errors=0
Assistant creation alerte deployee: commande `/create` fonctionnelle sur server
UX tuiles kimsufi-like: wizard creation et edition alertes avec presets capacite/prix, categories, connexions, suppression confirmee; admin ajouter/revoquer/reactiver en tuiles
Deploiement UX tuiles 2026-04-13: commit 4e95fc5 extrait dans /opt/diskcount, service redemarre, compileall distant OK, systemd active, dernier scan fetched=419 matched=0 notified=0 errors=0
Test pytest distant: non execute, le venv de production ne contient pas pytest
Correction UX capacites/sources 2026-04-13: capacites en multi-selection via `capacity_presets_json`; sources retirees de la creation/edition Telegram et conservees comme backend scanner/env
Deploiement correction capacites/sources 2026-04-13: commit f9ebe0c extrait dans /opt/diskcount, service redemarre, compileall distant OK, systemd active, migration `capacity_presets_json=True`, dernier scan fetched=420 matched=0 notified=0 errors=0
Guide Aide 2026-04-13: ajout des tuiles de documentation pour chaque fonction principale du bot
Deploiement Guide Aide 2026-04-13: commit 7889a5e extrait dans /opt/diskcount, service redemarre, compileall distant OK, systemd active, dernier scan fetched=429 matched=0 notified=0 errors=0
Migration SQLite: colonnes alerts.drive_categories_json, alerts.interfaces_json, alerts.capacity_presets_json, products.drive_category, products.interfaces_json presentes
```

```text
Telegram command menu
Default: start, menu, create, help, add, alerts, pause, resume, delete, set_max_price, set_capacity, test, status
Admin 123456789: start, menu, create, help, add, alerts, pause, resume, delete, set_max_price, set_capacity, test, status, users, allow, revoke
Tiles: /start, /menu et /help affichent des tuiles inline; actions Creer une alerte, Mes alertes, Scanner/Test, Aide, Admin; sous-menus avec Precedent et Accueil
Verification Bot API server: OK
Edition alertes: ouverture de chaque alerte depuis Mes alertes; ecrans type, etat, capacites multi-selection, prix, categories DiskPrices, connexions, pause/reprise, suppression confirmee
Verification Bot API server apres edition avancee: set_capacity present pour default et admin
Verification Bot API server apres UX tuiles: create present pour default et admin
Menage depot 2026-04-13: suppression des artefacts non suivis `bot_clean.py`, `diskcount/bot.py.backup`, archives zip et dossier `diskcount-deploy`; ajout des exclusions correspondantes dans `.gitignore`
```

Le dry-run CLI a ete execute dans un venv temporaire et avec une base SQLite temporaire hors du repo.

## Reste a configurer

| Priorite | Tache | Detail |
| --- | --- | --- |
| P0 | Creer le bot Telegram | Recuperer `TELEGRAM_BOT_TOKEN` depuis BotFather. |
| P0 | Recuperer l'ID utilisateur | Definir `TELEGRAM_ADMIN_USER_IDS` avec ton ID Telegram. |
| P1 | Configurer Dealabs | Ajouter les flux RSS d'alertes dans `DEALABS_RSS_URLS`. |
| P1 | Configurer eBay | Creer des credentials eBay Developer puis definir `EBAY_CLIENT_ID`, `EBAY_CLIENT_SECRET`, `EBAY_SEARCH_QUERIES`. |
| P1 | Configurer Idealo | Ajouter des flux dans `IDEALO_FEED_URLS` ou des pages publiques dans `IDEALO_PAGE_URLS`. |
| P1 | Configurer leDenicheur | Ajouter des flux dans `LEDENICHEUR_FEED_URLS` ou des pages publiques dans `LEDENICHEUR_PAGE_URLS`. |
| P1 | Configurer leboncoin | Ajouter des flux/alertes compatibles dans `LEBONCOIN_FEED_URLS`. |
| P1 | Ajouter Keepa si besoin | Definir `KEEPA_API_KEY` et `KEEPA_ASINS`; sinon laisser vide. |
| P2 | Observer les premieres donnees | Laisser tourner pour constituer l'historique rolling 30 jours. |

## Commande d'alerte recommandee

```text
/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used discount=5 cooldown=24
```

## Risques suivis

| Risque | Niveau | Mitigation |
| --- | --- | --- |
| RSS Dealabs peu structure | Moyen | Parser seulement les entrees avec prix + capacite detectables. |
| Keepa API incomplet sans ASIN | Moyen | Connecteur optionnel; DiskPrices reste source principale. |
| Prix habituel immature au demarrage | Faible | `max_eur_tb` declenche immediatement; remise rolling active apres historique. |
| Pages dynamiques sans donnees parseables | Moyen | HTTP direct puis fallback Playwright headless sur pages publiques configurees; pas de contournement CAPTCHA/blocage d'acces. |
