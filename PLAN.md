# Plan de projet DiskCount

## Objectif

Creer un bot Telegram auto-heberge sur VPS Debian pour surveiller les bons plans HDD/SSD et notifier les utilisateurs selon des filtres configurables depuis Telegram.

Le bot doit couvrir le cas principal suivant :

```text
Je veux etre notifie des bons plans sur des disques rotational neufs ou d'occasion,
a 16 To minimum, sous 20 EUR/To, et/ou avec une remise minimale de 5% par rapport
au prix habituel observe sur 30 jours.
```

## Perimetre v1

- Bot Telegram interactif avec commandes `/start`, `/menu`, `/help`, `/add`, `/alerts`, `/pause`, `/resume`, `/delete`, `/test`, `/status`.
- Menu de commandes Telegram via `setMyCommands`, avec descriptions visibles quand l'utilisateur tape `/`.
- Navigation Telegram en tuiles inline via `/start`, `/menu` et `/help`, avec categories `Alertes`, `Scan`, `Sources`, `Aide`, `Admin`.
- Sous-menus Telegram avec explications contextuelles et boutons `Precedent` / `Accueil` en bas.
- Commande Telegram `/set_max_price` pour modifier rapidement le seuil EUR/To d'une alerte.
- Commande Telegram `/set_capacity` pour modifier la plage min/max de stockage.
- Edition inline des alertes depuis `Mes alertes` : modifier, pauser/reprendre, supprimer, cocher HDD/SSD, new/used, categories DiskPrices et connexions.
- Filtres DiskPrices par categories `external_3_5`, `external_2_5`, `internal_3_5`, `internal_2_5`, `internal_hybrid`, `internal_sas`, `external_ssd`, `internal_ssd`, `m2_sata`, `m2_nvme`, `u2_u3`.
- Filtres de connectique `sata`, `sas`, `nvme`, `usb`.
- Seuil SSD en EUR/Go via `max_eur_gb`, converti en EUR/To pour les regles internes.
- Panel admin Telegram via commandes `/users`, `/allow <id> <nom>`, `/revoke <id>`.
- Acces utilisateur persistant dans SQLite, avec super-admin defini par `TELEGRAM_ADMIN_USER_IDS`.
- Alertes proprietaires par utilisateur Telegram : chaque utilisateur autorise gere ses propres alertes uniquement.
- CLI de pilotage inspiree de kimsufi-notifier : `check`, `list`, `scan`, `run`, `init-db`.
- Stockage local SQLite des alertes, produits, observations de prix et notifications envoyees.
- Baseline de prix rolling 30 jours basee sur la mediane des observations precedentes.
- Notification immediate possible quand `max_eur_tb` est atteint, meme sans historique de 30 jours.
- Anti-spam par cooldown d'alerte et re-notification anticipee seulement en cas de nouvelle baisse significative.
- Notifications Telegram avec bouton direct vers l'offre et delai configurable entre messages.
- Assistant de creation d'alerte conversationnel (commande `/create`) avec etapes pour le nom, la capacite, le prix, le type de media, l'etat, les categories, les interfaces et les sources.
- Deploiement Debian via `systemd`, venv Python, secrets dans `/etc/diskcount.env`, donnees dans `/var/lib/diskcount`.

## Sources v1

- `diskprices` : source principale, parsing de `https://diskprices.com/?locale=fr`.
- `dealabs` : flux RSS d'alertes configures par l'utilisateur dans `DEALABS_RSS_URLS`.
- `ebay` : API officielle eBay Browse, active si `EBAY_CLIENT_ID`, `EBAY_CLIENT_SECRET` et `EBAY_SEARCH_QUERIES` sont definis.
- `idealo` : flux/alertes configures dans `IDEALO_FEED_URLS`, sans scraping de pages.
- `ledenicheur` : flux/alertes configures dans `LEDENICHEUR_FEED_URLS`, sans scraping de pages.
- `leboncoin` : flux/alertes configures dans `LEBONCOIN_FEED_URLS`, sans scraping de pages.
- `keepa` : connecteur API optionnel si `KEEPA_API_KEY` et `KEEPA_ASINS` sont definis.

## Architecture

- `diskcount/config.py` : configuration par variables d'environnement.
- `diskcount/parsing.py` : normalisation prix, capacite, condition, type de disque et ASIN.
- `diskcount/sources/` : collecteurs DiskPrices, Dealabs RSS, flux configures, eBay Browse API et Keepa API.
- `diskcount/db.py` : modeles SQLAlchemy et repository SQLite.
- `diskcount/rules.py` : matching d'alertes, baseline, seuils et cooldown.
- `diskcount/scanner.py` : orchestration collecte/evaluation/notification.
- `diskcount/bot.py` : commandes Telegram, parsing des alertes et panel admin.
- `diskcount/cli.py` : commandes `init-db`, `scan --dry-run`, `run`.
- `deploy/` : fichiers d'environnement et unit `systemd`.
- `tests/` : tests unitaires et dry-run scanner.

## Commandes cible

Exemple d'alerte :

```text
/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used discount=5 sources=diskprices,dealabs,ebay,leboncoin cooldown=24
```

Commandes de verification :

```bash
python -m diskcount init-db
python -m diskcount check
python -m diskcount list --min-tb 16 --max-eur-tb 20 --media rotational
python -m diskcount run
```

## Tests et acceptance criteria

- Parsing DiskPrices : prix, capacite, technologie, condition et ASIN.
- Parsing Dealabs RSS : titre, lien, prix, capacite, type disque et condition.
- Parsing flux configures Idealo/leDenicheur/leboncoin : titre, lien, prix, capacite, type disque et condition par defaut.
- Parsing eBay Browse API : item ID, lien, prix EUR, capacite, type disque et condition.
- Normalisation : prix FR, capacites To/Go, HDD/SSD, neuf/occasion.
- Regles : seuil EUR/To sans historique, remise rolling 30 jours, cooldown anti-spam.
- Repository : deduplication produit et mediane rolling 30 jours.
- Bot : parsing `/add` et filtrage des utilisateurs autorises.
- Bot : menu Telegram `/` pour commandes utilisateur et scope admin.
- Bot : navigation inline en tuiles pour les commandes principales et les commandes admin.
- Bot : categories de menu avec explications, `Precedent` et `Accueil`.
- Bot : modification de seuil avec `/set_max_price`.
- Bot : modification de plage de stockage avec `/set_capacity`.
- Bot : edition inline d'une alerte existante depuis la liste.
- Bot : assistant de creation d'alerte interactif via `/create` avec validation par etapes.
- Regles : filtres categories DiskPrices et connexions.
- Bot : gestion admin des utilisateurs avec `/users`, `/allow`, `/revoke`.
- Repository : isolation des alertes par `owner_user_id`, avec migration SQLite automatique pour les bases existantes.
- CLI : filtrage `list` par capacite, EUR/To, technologie et etat.
- Scanner : `dry_run=True` simule les notifications sans persister les produits.

## Deploiement Debian

1. Creer l'utilisateur systeme `diskcount`.
2. Installer le projet dans `/opt/diskcount`.
3. Creer un venv Python 3.11+.
4. Copier `deploy/diskcount.env.example` vers `/etc/diskcount.env`.
5. Definir `TELEGRAM_BOT_TOKEN` et `TELEGRAM_ADMIN_USER_IDS`.
6. Copier `deploy/diskcount.service` vers `/etc/systemd/system/diskcount.service`.
7. Executer `systemctl daemon-reload`, `enable --now diskcount`, puis surveiller avec `journalctl -u diskcount -f`.

## Evolutions prevues

- Import/export YAML des alertes pour sauvegarde manuelle.
- Meilleure extraction Dealabs sur les titres complexes.
- Support de sources additionnelles uniquement via API/RSS ou pages publiques explicitement compatibles.
- Dashboard Web local optionnel si l'usage depasse Telegram.

## Regle de maintenance

- Toute evolution fonctionnelle, configurationnelle ou de deploiement doit mettre a jour les `.md` concernes.
- Chaque lot termine doit etre commit puis pousse sur le repo prive GitHub `DiskCount/DiskCount`.
