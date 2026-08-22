# DiskCount — spécification produit

## But

DiskCount centralise les offres HDD/SSD, conserve leur historique de prix et envoie des alertes configurées depuis l'interface web.

## Déploiement imposé

- L'application est toujours auto-hébergée en Docker sur ZimaOS.
- `compose.yaml` est la base locale ; `compose.prod.yaml` est la variante de production avec PostgreSQL persistant et Byparr derrière WireGuard.
- Le service web écoute sur le port `47832` et reste protégé par `WEB_ADMIN_PASSWORD`.
- PostgreSQL est l'unique stockage. Les migrations restent numérotées, append-only et exécutées au démarrage.

## Expérience web

- Direction visuelle : interface flat design avec accents pixel art (bordures dures, police pixel pour le brand et les titres, corps monospace lisible). Thèmes clair et sombre. Aucun contenu, logo ou actif de DropReference n'est repris.
- Navigation principale : Tableau de bord, Produits, Sites, Logs, Baisses de prix, Créer une alerte, Discord, Configuration. Marché, Europe, Qualité et Métriques restent accessibles depuis le tableau de bord sans surcharger le menu.
- Tableau de bord : état du service, sources, dernier et prochain scan, produits, observations et notifications.
- Produits : filtres, prix actuel, prix par To, tendance, source, date du dernier refresh, fiche d'historique et lien direct vers le marchand.
- Sites : état du dernier scan, offres, produits, observations, rejets, durée, breaker, erreur et médiane EUR/To par fournisseur.
- Logs : succès en vert, informations en bleu, avertissements en orange et erreurs en rouge pour le dernier scan.
- Alertes : création directe dans l'app, seuil de prix, remise, délai, support, état, capacités, sources et mots-clés ; pause, reprise, suppression et activation Discord depuis le site.
- Configuration : secrets masqués et paramètres persistés en base.
- Responsive : ordinateur, tablette et mobile sans dépendance JavaScript obligatoire.

## Étude comparative — 22 août 2026

### Produits observés

- [DropReference](https://dropreference.com/fr/stock/gpu) : catalogue regroupé par famille, recherche, facettes très riches, disponibilité, vendeurs, indicateur de qualité d'offre et historique. À adapter en familles HDD/SSD, capacité, interface, usage et méthode d'enregistrement.
- [GPUTracker](https://www.gputracker.eu/fr/category/1/cartes-graphiques) : aperçu donnant immédiatement le meilleur prix par famille, onglets baisses de prix/offres populaires, recherche avancée, pays de livraison et lien marchand direct. À retenir : accès rapide au meilleur vendeur et vue dédiée aux baisses.
- [GPUPrix](https://gpuprix.com/fr/france/gpus) : tableau comparatif compact, filtres simples, comparaison, meilleur prix par pays et score de valeur. Pour le stockage, le score principal reste le prix par To, complété par la tendance historique et la qualité des métadonnées ; un score opaque n'est pas souhaitable.

### Projets GitHub étudiés

- [PriceBuddy](https://github.com/jez500/pricebuddy) : auto-hébergement Docker, regroupement de plusieurs annonces pour un produit, disponibilité détaillée, historique, prix unitaire et notifications multi-canaux. Référence fonctionnelle la plus proche, sans reprendre sa stack Laravel.
- [PriceGhost](https://github.com/clucraft/PriceGhost) : stratégies d'extraction multiples, validation des prix ambigus, actualisation manuelle, alertes de retour en stock et test de chaque canal. À retenir plus tard : bouton de test des notifications et visibilité des désaccords d'extraction. L'IA de scraping reste hors périmètre tant que les extracteurs déterministes suffisent.
- [price-per-tb](https://github.com/cadencejames/price-per-tb) : comparateur HDD/SSD multi-marchands trié par prix par To. Il confirme la métrique centrale, mais son rapport statique et Selenium ne remplacent pas DiskCount.
- [hdd-price-tracker](https://github.com/ikr7/hdd-price-tracker) : prototype spécialisé HDD encore peu documenté ; aucune architecture à reprendre.
- [hddhunt-price-index](https://github.com/AdamDudley/hddhunt-price-index) : index quotidien par tranche de capacité. Idée retenue pour une future vue d'indice du marché, après accumulation d'un historique local suffisant.

### Adaptation au stockage

- Facettes principales : HDD/SSD, capacité, marque, état, interface, usage (`nas`, `desktop`, `datacenter`, `surveillance`), CMR/SMR et marchand.
- Classement par défaut : prix par To, puis fraîcheur. Le prix absolu reste visible.
- Une fiche produit regroupe à terme les offres équivalentes de plusieurs vendeurs ; l'identité doit reposer sur marque + modèle + capacité, pas uniquement sur le titre marchand.
- Une bonne affaire est évaluée contre l'historique du même produit et de sa tranche de capacité, jamais contre tous les disques confondus.
- L'état de stock doit évoluer de disponible vers indisponible, retour en stock ou discontinu quand les sources fournissent cette information.
- Les alertes restent multi-critères. L'application est la source de vérité et Discord est une sortie optionnelle cochée alerte par alerte.

### Ordre d'implémentation

1. Terminé : recherche et facettes stockage sur le catalogue, prix par To, fraîcheur, historique et liens vendeurs.
2. Terminé : regroupement multi-vendeurs fiable par marque + référence modèle + capacité, et vue des baisses de prix actuelles.
3. Terminé : disponibilité explicite après trois scans source réussis manquants, retour en stock, et test Discord depuis le site.
4. Terminé : indice quotidien par tranche de capacité et comparaison européenne. Les messages privés Discord ne sont pas retenus : le salon configuré couvre le besoin actuel sans complexité ni permission supplémentaire.
5. Terminé : catalogue regroupé par marque + SKU/modèle + capacité, avec image, nombre de vendeurs, filtres SQL, pagination et fiche famille multi-vendeurs.
6. Terminé : URLs de listing par défaut pour LDLC, TopAchat, Grosbill, Fnac, Boulanger, Cdiscount et Rue du Commerce ; un CAPTCHA (Darty) apparaît en erreur rouge sur Sites et Logs plutôt qu'en zéro silencieux. Discord reste en attente, sans identifiants.

## Disponibilité — version actuelle

- Une offre observée est `available` et remet son compteur d'absence à zéro.
- Une offre devient `unavailable` après trois scans réussis de sa source où elle est absente.
- Une erreur, un circuit breaker, un scan vide ou un lot entièrement rejeté ne modifie jamais la disponibilité.
- Une nouvelle observation restaure immédiatement l'offre et le mécanisme existant de retour en stock peut déclencher l'alerte.
- L'état `discontinued` n'est pas déduit : aucune source actuelle ne permet de distinguer de façon fiable un retrait définitif d'une absence prolongée.

## Fournisseurs — version actuelle

- Marchands français directs : Alternate FR, Boulanger, Cdiscount, Corsair FR, Cybertek, Darty, Fnac, Grosbill, LDLC, Materiel.net, PCComponentes FR, Rue du Commerce, TopAchat et Topbiz.
- Amazon FR/DE/ES/IT : Keepa et agrégateurs autorisés, avec validation stricte du domaine final `amazon.*`.
- DiskPrices, PricePerGig et PricePerTB ne peuvent jamais introduire un marchand non Amazon.
- PricePerGig utilise son API PostgREST filtrée par `marketplace=eq.amazon.fr`, conserve le lien exact fourni, importe la date `last_updated` et rafraîchit les données toutes les quatre heures, donc sous la limite de cache de 24 heures.
- AliExpress est exclu. Asus Shop FR ne propose que des boîtiers dans la catégorie stockage observée et Nvidia FR ne vend pas de HDD/SSD grand public ; ils apparaissent comme hors périmètre plutôt que comme de faux fournisseurs de prix.

## Alertes — version actuelle

- Les alertes sont créées, activées, mises en pause et supprimées uniquement dans l'application.
- Aucun service de messagerie ne peut créer ou modifier une alerte.
- Le scanner collecte les prix et conserve l'historique même si Discord n'est pas configuré.
- Chaque déclenchement est enregistré dans l'application, que le relais Discord soit activé ou non.

## Discord — version actuelle

- L'intégration est implantée mais laissée dormante : aucun token ni salon n'est requis et aucune alerte ne coche Discord par défaut.
- La page `/discord` enregistre le token du bot et l'identifiant du salon cible ; le token reste masqué.
- La configuration est appliquée immédiatement, sans redémarrage du conteneur.
- Le bot est strictement sortant : il relaie les résultats des alertes de l'application, sans commande ni gestion distante.
- Chaque alerte possède une case `Envoyer sur Discord`. Une alerte non cochée ne produit aucun message Discord.
- La destination actuelle est un salon unique. Les messages privés restent hors périmètre jusqu'à un besoin concret.

## Critères de validation

- `go test ./...`, `go vet ./...` et `docker compose config` passent.
- Une alerte créée sur `/alerts` est persistée et peut être mise en pause ou supprimée.
- `/products` montre une carte par famille (image ou placeholder, SKU, meilleur prix, nombre de vendeurs) et un lien vers la fiche ; `/product` compare les marchands.
- Sans configuration Discord, les scans, l'historique et les déclenchements locaux continuent ; une erreur de relais reste visible.
- L'interface est vérifiée en thème sombre et clair aux largeurs desktop et mobile.

## Validation opérationnelle

- Déploiement ZimaOS validé après sauvegarde PostgreSQL : migration 7 (sku, image_url) appliquée, données conservées. Discord reste dormant.
- L'envoi Discord réel se valide depuis `/discord` dès qu'un token et un salon sont renseignés ; aucun identifiant Discord de production n'est stocké dans le dépôt.
