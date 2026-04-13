# Dashboard de realisation DiskCount

Derniere mise a jour : 2026-04-13

## Statut global

| Lot | Statut | Notes |
| --- | --- | --- |
| Squelette Python | Termine | `pyproject.toml`, package `diskcount`, CLI `python -m diskcount`. |
| Configuration | Termine | Variables d'environnement via `pydantic-settings`, `.env` supporte en local; listes CSV supportees. |
| Stockage SQLite | Termine | Alertes, produits, observations, notifications, subscribers. |
| Collecteur DiskPrices | Termine | Dry-run live verifie : 429 offres parsees, 0 erreur. |
| Collecteur Dealabs RSS | Termine | Parsing RSS configure par `DEALABS_RSS_URLS`. |
| Collecteur eBay | Termine | API officielle Browse, active avec credentials eBay. |
| Flux Idealo | Termine v1 | Via `IDEALO_FEED_URLS`, sans scraping de pages. |
| Flux leDenicheur | Termine v1 | Via `LEDENICHEUR_FEED_URLS`, sans scraping de pages. |
| Flux leboncoin | Termine v1 | Via `LEBONCOIN_FEED_URLS`, sans scraping de pages. |
| Connecteur Keepa API | Termine v1 | Optionnel, actif seulement avec `KEEPA_API_KEY` + `KEEPA_ASINS`. |
| Regles de notification | Termine | Seuil EUR/To, remise rolling 30 jours, cooldown, baisse significative. |
| Bot Telegram | Termine | Commandes demandees implementees. |
| Menu commandes Telegram | Termine | `setMyCommands` configure un menu `/` utilisateur et un menu admin pour `TELEGRAM_ADMIN_USER_IDS`. |
| Tuiles commandes Telegram | Termine | `/start`, `/menu` et `/help` affichent une navigation inline par categories; sous-menus avec `Precedent` et `Accueil`. |
| Edition alertes Telegram | Termine | `Mes alertes` ouvre chaque alerte comme tuile; modification cliquable de type, etat, capacite, prix, categories, connexions, sources, pause/reprise et suppression confirmee. |
| Filtres DiskPrices avances | Termine | Categories interne/externe/form factor, connectiques SATA/SAS/NVMe/USB, `max_eur_gb` SSD converti en EUR/To. |
| Panel admin Telegram | Termine | `/users`, `/allow <id> <nom>`, `/revoke <id>` avec super-admin env. |
| Alertes par utilisateur | Termine | Chaque utilisateur autorise possede ses alertes et ne peut modifier/supprimer que les siennes. |
| Boutons Telegram | Termine | Les alertes incluent un bouton direct vers l'offre. |
| Assistant de creation d'alerte | Termine | Commande `/create` et bouton `Creer une alerte`; wizard 100% tuiles avec presets HDD/SSD, prix, categories, connexions, sources et recapitulatif. |
| CLI kimsufi-like | Termine | `check`, `list`, `scan`, `run`, `init-db`. |
| Deploiement Debian | Termine | Fichiers `deploy/diskcount.service` et `deploy/diskcount.env.example`. |
| Deploiement server | Termine | Service `diskcount` actif sur `<REDACTED_IP>`; bot `@DiskCount_bot` en polling. |
| Tests | Termine | 31 tests passent. |
| Documentation | En cours | README, plan projet et dashboard presents. |
| Workflow projet | Actif | Toute evolution doit mettre a jour les `.md` concernes et etre poussee sur le repo prive GitHub. |
| Menage depot | Termine | Artefacts locaux non suivis supprimes; `.gitignore` couvre archives zip, backups et exports temporaires. |
| Acces VPS SSH server | Debloque | Seule l'IP client `<REDACTED_IP>` est en ignoreip fail2ban et autorisee UFW sur `<SSH_PORT>/tcp`; fail2ban reste actif pour les autres IPs. |
| Acces VPS SSH server | Debloque | `<REDACTED_IP>` est en ignoreip fail2ban et autorisee UFW sur `<REDACTED_IP>:<SSH_PORT>`. |
| Acces VPS SSH server | Debloque | `<REDACTED_IP>` est en ignoreip fail2ban et autorisee UFW sur `<REDACTED_IP>:<SSH_PORT>`. |

## Verification executee

```text
python -m compileall -q diskcount tests
Resultat: OK
```

```text
pytest -q
Resultat: 31 passed in 1.65s
```

```text
python -m diskcount init-db
python -m diskcount check
python -m diskcount list --min-tb 16 --max-eur-tb 20 --media rotational
Resultat: fetched=431 matched=0 notified=0 dry_run_notifications=0 errors=0
Top live liste: 24 To a 19.36 EUR/To, 18 To a 19.88 EUR/To, 28 To a 19.95 EUR/To
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
UX tuiles kimsufi-like: wizard creation et edition alertes avec presets capacite/prix, sources, categories, connexions, suppression confirmee; admin ajouter/revoquer/reactiver en tuiles
Deploiement UX tuiles 2026-04-13: commit 4e95fc5 extrait dans /opt/diskcount, service redemarre, compileall distant OK, systemd active, dernier scan fetched=419 matched=0 notified=0 errors=0
Test pytest distant: non execute, le venv de production ne contient pas pytest
Migration SQLite: colonnes alerts.drive_categories_json, alerts.interfaces_json, products.drive_category, products.interfaces_json presentes
```

```text
Telegram command menu
Default: start, menu, create, help, add, alerts, pause, resume, delete, set_max_price, set_capacity, test, status
Admin 123456789: start, menu, create, help, add, alerts, pause, resume, delete, set_max_price, set_capacity, test, status, users, allow, revoke
Tiles: /start, /menu et /help affichent des tuiles inline; actions Creer une alerte, Mes alertes, Scanner/Test, Sources, Aide, Admin; sous-menus avec Precedent et Accueil
Verification Bot API server: OK
Edition alertes: ouverture de chaque alerte depuis Mes alertes; ecrans type, etat, capacite, prix, categories DiskPrices, connexions, sources, pause/reprise, suppression confirmee
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
| P1 | Configurer Idealo | Ajouter des flux/alertes compatibles dans `IDEALO_FEED_URLS`. |
| P1 | Configurer leDenicheur | Ajouter des flux/alertes compatibles dans `LEDENICHEUR_FEED_URLS`. |
| P1 | Configurer leboncoin | Ajouter des flux/alertes compatibles dans `LEBONCOIN_FEED_URLS`. |
| P1 | Ajouter Keepa si besoin | Definir `KEEPA_API_KEY` et `KEEPA_ASINS`; sinon laisser vide. |
| P2 | Observer les premieres donnees | Laisser tourner pour constituer l'historique rolling 30 jours. |

## Commande d'alerte recommandee

```text
/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used discount=5 sources=diskprices,dealabs,ebay,leboncoin cooldown=24
```

## Risques suivis

| Risque | Niveau | Mitigation |
| --- | --- | --- |
| RSS Dealabs peu structure | Moyen | Parser seulement les entrees avec prix + capacite detectables. |
| Keepa API incomplet sans ASIN | Moyen | Connecteur optionnel; DiskPrices reste source principale. |
| Prix habituel immature au demarrage | Faible | `max_eur_tb` declenche immediatement; remise rolling active apres historique. |
| Sites non compatibles scraping | Faible | Idealo/leDenicheur/leboncoin consommes uniquement via flux configures; eBay via API officielle. |
