from __future__ import annotations

import shlex
from dataclasses import dataclass
from decimal import Decimal

from aiogram import Bot
from aiogram import Dispatcher, Router
from aiogram.filters import Command, CommandObject, CommandStart
from aiogram.types import (
    BotCommand,
    BotCommandScopeChat,
    BotCommandScopeDefault,
    CallbackQuery,
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    KeyboardButton,
    Message,
    ReplyKeyboardMarkup,
    ReplyKeyboardRemove,
)

from .config import Settings
from .db import Alert, Repository
from .scanner import Scanner

VALID_CONDITIONS = {"new", "used"}
VALID_MEDIA_TYPES = {"rotational", "solid_state"}
VALID_SOURCES = {"diskprices", "dealabs", "idealo", "ledenicheur", "leboncoin", "ebay", "keepa"}

USER_COMMANDS: tuple[tuple[str, str], ...] = (
    ("start", "Demarrer le bot et enregistrer le chat"),
    ("menu", "Ouvrir la navigation par tuiles"),
    ("help", "Afficher les exemples et les filtres disponibles"),
    ("add", "Ajouter une alerte de prix disque"),
    ("alerts", "Lister tes alertes"),
    ("pause", "Mettre une alerte en pause"),
    ("resume", "Reactiver une alerte"),
    ("delete", "Supprimer une alerte"),
    ("set_max_price", "Modifier le seuil EUR/To d'une alerte"),
    ("test", "Lancer un scan de test sans notifier"),
    ("status", "Afficher l'etat du bot et des sources"),
)

ADMIN_COMMANDS: tuple[tuple[str, str], ...] = (
    ("users", "Lister les utilisateurs autorises"),
    ("allow", "Autoriser un utilisateur par ID Telegram"),
    ("revoke", "Retirer l'acces d'un utilisateur"),
)


@dataclass(frozen=True)
class AlertArgs:
    name: str
    min_capacity_tb: float | None
    max_capacity_tb: float | None
    conditions: list[str]
    media_types: list[str]
    sources: list[str]
    max_price_per_tb: Decimal | None
    min_discount_pct: float
    cooldown_hours: int


def is_env_admin(settings: Settings, user_id: int | None) -> bool:
    if user_id is None:
        return False
    return user_id in settings.telegram_admin_user_ids


def is_authorized(settings: Settings, repository: Repository | None, user_id: int | None) -> bool:
    if user_id is None:
        return False
    if is_env_admin(settings, user_id):
        return True
    if user_id in settings.telegram_allowed_user_ids:
        return True
    return bool(repository and repository.is_user_allowed(user_id))


def build_bot_commands(include_admin: bool = False) -> list[BotCommand]:
    commands = list(USER_COMMANDS)
    if include_admin:
        commands.extend(ADMIN_COMMANDS)
    return [BotCommand(command=command, description=description) for command, description in commands]


def build_main_keyboard(include_admin: bool = False) -> ReplyKeyboardMarkup:
    rows = [
        [KeyboardButton(text="/menu"), KeyboardButton(text="/alerts"), KeyboardButton(text="/add")],
    ]
    if include_admin:
        rows.append([KeyboardButton(text="/users"), KeyboardButton(text="/allow"), KeyboardButton(text="/revoke")])
    return ReplyKeyboardMarkup(keyboard=rows, resize_keyboard=True, is_persistent=False)


def build_menu_keyboard(view: str = "home", include_admin: bool = False) -> InlineKeyboardMarkup:
    def button(text: str, data: str) -> InlineKeyboardButton:
        return InlineKeyboardButton(text=text, callback_data=data)

    nav = [[button("Precedent", _menu_parent(view)), button("Accueil", "menu:home")]]
    keyboards: dict[str, list[list[InlineKeyboardButton]]] = {
        "home": [
            [button("Alertes", "menu:alerts"), button("Scan", "menu:scan")],
            [button("Sources", "menu:sources"), button("Aide", "menu:help")],
        ],
        "alerts": [
            [button("Mes alertes", "menu:alerts:list"), button("Creer", "menu:alerts:add")],
            [button("Pause", "menu:alerts:pause"), button("Reprendre", "menu:alerts:resume")],
            [button("Supprimer", "menu:alerts:delete"), button("Prix/To", "menu:alerts:price")],
            *nav,
        ],
        "alerts:list": nav,
        "alerts:add": nav,
        "alerts:pause": nav,
        "alerts:resume": nav,
        "alerts:delete": nav,
        "alerts:price": nav,
        "scan": [
            [button("Statut", "menu:scan:status"), button("Test", "menu:scan:test")],
            *nav,
        ],
        "scan:status": nav,
        "scan:test": nav,
        "sources": [
            [button("DiskPrices", "menu:sources:diskprices"), button("Dealabs", "menu:sources:dealabs")],
            [button("eBay", "menu:sources:ebay"), button("leboncoin", "menu:sources:leboncoin")],
            [button("Idealo", "menu:sources:idealo"), button("leDenicheur", "menu:sources:ledenicheur")],
            [button("Keepa", "menu:sources:keepa")],
            *nav,
        ],
        "sources:diskprices": nav,
        "sources:dealabs": nav,
        "sources:ebay": nav,
        "sources:leboncoin": nav,
        "sources:idealo": nav,
        "sources:ledenicheur": nav,
        "sources:keepa": nav,
        "help": [
            [button("Exemple /add", "menu:alerts:add"), button("Filtres", "menu:help:filters")],
            [button("Commandes", "menu:help:commands")],
            *nav,
        ],
        "help:filters": nav,
        "help:commands": nav,
        "admin": [
            [button("Utilisateurs", "menu:admin:users"), button("Autoriser", "menu:admin:allow")],
            [button("Revoquer", "menu:admin:revoke")],
            *nav,
        ],
        "admin:users": nav,
        "admin:allow": nav,
        "admin:revoke": nav,
    }
    rows = keyboards.get(view, keyboards["home"])
    if view == "home" and include_admin:
        rows = [*rows, [button("Admin", "menu:admin")]]
    return InlineKeyboardMarkup(inline_keyboard=rows)


def _menu_parent(view: str) -> str:
    if view == "home":
        return "menu:home"
    if ":" not in view:
        return "menu:home"
    return f"menu:{view.rsplit(':', 1)[0]}"


def menu_home_text(include_admin: bool = False) -> str:
    admin_line = "\nAdmin: gere les acces autorises." if include_admin else ""
    return (
        "DiskCount\n\n"
        "Surveille les bons plans HDD/SSD et notifie quand une offre respecte tes filtres.\n\n"
        "Choisis une categorie:\n"
        "- Alertes: creer, voir, pauser, supprimer, regler le prix EUR/To.\n"
        "- Scan: verifier l'etat du bot ou lancer un test sans notification.\n"
        "- Sources: comprendre DiskPrices, Dealabs, eBay, leboncoin, Idealo, leDenicheur et Keepa.\n"
        "- Aide: exemples et filtres disponibles."
        f"{admin_line}"
    )


def menu_static_text(view: str) -> str:
    texts = {
        "alerts": (
            "Alertes\n\n"
            "Chaque utilisateur autorise possede ses propres alertes. Tu peux les creer, les lister, les pauser, "
            "les reprendre, modifier leur seuil EUR/To ou les supprimer."
        ),
        "alerts:add": (
            "Creer une alerte\n\n"
            "Commande:\n"
            "/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
            "discount=5 sources=diskprices,dealabs,ebay,leboncoin cooldown=24\n\n"
            "Minimum utile pour ton cas: min_tb=16 max_eur_tb=20 media=rotational condition=new,used."
        ),
        "alerts:pause": (
            "Mettre en pause\n\n"
            "Commande: /pause 1\n"
            "Remplace 1 par l'ID affiche dans Mes alertes. L'alerte reste en base mais ne notifie plus."
        ),
        "alerts:resume": (
            "Reprendre une alerte\n\n"
            "Commande: /resume 1\n"
            "Remplace 1 par l'ID affiche dans Mes alertes. Les prochains scans pourront de nouveau notifier."
        ),
        "alerts:delete": (
            "Supprimer une alerte\n\n"
            "Commande: /delete 1\n"
            "Remplace 1 par l'ID affiche dans Mes alertes. Tu ne peux supprimer que tes propres alertes."
        ),
        "alerts:price": (
            "Modifier le prix EUR/To\n\n"
            "Commande: /set_max_price 1 18.5\n"
            "Pour enlever le seuil: /set_max_price 1 none\n\n"
            "Ce seuil peut declencher une notification des la premiere observation, meme sans historique 30 jours."
        ),
        "scan": (
            "Scan et statut\n\n"
            "Statut donne les compteurs et les sources actives. Test lance un dry-run: le bot collecte et calcule "
            "les matchs pour tes alertes, sans envoyer de vraie notification."
        ),
        "scan:test": (
            "Test en dry-run\n\n"
            "Commande: /test\n"
            "Le scan est limite a tes alertes. Il ne persiste pas de nouvelle observation et n'envoie pas d'alerte."
        ),
        "sources": (
            "Sources\n\n"
            "DiskPrices est la source principale. Dealabs, Idealo, leDenicheur et leboncoin passent par des flux "
            "configures. eBay utilise l'API officielle. Keepa est optionnel pour enrichir l'historique Amazon."
        ),
        "sources:diskprices": (
            "DiskPrices\n\n"
            "Source principale sur le marche FR. Le bot lit les offres HDD/SSD, prix, capacite, etat, type disque "
            "et lien marchand quand les informations sont presentes."
        ),
        "sources:dealabs": (
            "Dealabs\n\n"
            "Utilise des flux RSS d'alertes que tu configures dans DEALABS_RSS_URLS. Pas de scraping agressif."
        ),
        "sources:ebay": (
            "eBay\n\n"
            "Utilise l'API officielle Browse si EBAY_CLIENT_ID, EBAY_CLIENT_SECRET et EBAY_SEARCH_QUERIES sont definis."
        ),
        "sources:leboncoin": (
            "leboncoin\n\n"
            "Consomme uniquement des flux/alertes configures dans LEBONCOIN_FEED_URLS. Pas de scraping de pages."
        ),
        "sources:idealo": (
            "Idealo\n\n"
            "Consomme uniquement des flux/alertes compatibles dans IDEALO_FEED_URLS. Pas de scraping de pages."
        ),
        "sources:ledenicheur": (
            "leDenicheur\n\n"
            "Consomme uniquement des flux/alertes compatibles dans LEDENICHEUR_FEED_URLS. Pas de scraping de pages."
        ),
        "sources:keepa": (
            "Keepa\n\n"
            "Connecteur API optionnel. Il ne s'active que si KEEPA_API_KEY et KEEPA_ASINS sont definis."
        ),
        "help": (
            "Aide\n\n"
            "Utilise les tuiles pour naviguer. Les actions qui demandent un ID affichent la commande exacte a envoyer."
        ),
        "help:filters": (
            "Filtres d'alerte\n\n"
            "name: nom libre.\n"
            "min_tb, max_tb: capacite en To.\n"
            "max_eur_tb: prix maximum par To.\n"
            "condition: new, used ou new,used.\n"
            "media: rotational ou solid_state.\n"
            "sources: diskprices, dealabs, idealo, ledenicheur, leboncoin, ebay, keepa.\n"
            "discount: remise minimale face au prix habituel 30 jours.\n"
            "cooldown: delai en heures avant une re-notification."
        ),
        "help:commands": (
            "Commandes\n\n"
            "/menu ouvre ces tuiles.\n"
            "/alerts liste tes alertes.\n"
            "/add cree une alerte.\n"
            "/pause, /resume, /delete gerent une alerte par ID.\n"
            "/set_max_price modifie le seuil EUR/To.\n"
            "/test lance un dry-run.\n"
            "/status affiche l'etat du bot."
        ),
        "admin": (
            "Admin\n\n"
            "Ces actions sont reservees aux IDs dans TELEGRAM_ADMIN_USER_IDS. Elles gerent qui peut utiliser le bot."
        ),
        "admin:allow": (
            "Autoriser un utilisateur\n\n"
            "Commande: /allow 123456789 Nom custom\n"
            "Le nom custom sert a reconnaitre la personne dans /users."
        ),
        "admin:revoke": (
            "Revoquer un utilisateur\n\n"
            "Commande: /revoke 123456789\n"
            "L'utilisateur ne pourra plus discuter avec le bot, mais l'historique reste en base."
        ),
    }
    return texts.get(view, menu_home_text())


async def configure_bot_commands(bot: Bot, settings: Settings) -> None:
    await bot.set_my_commands(build_bot_commands(), scope=BotCommandScopeDefault())
    for admin_id in settings.telegram_admin_user_ids:
        await bot.set_my_commands(
            build_bot_commands(include_admin=True),
            scope=BotCommandScopeChat(chat_id=admin_id),
        )


def parse_alert_args(text: str | None) -> AlertArgs:
    if not text or not text.strip():
        raise ValueError("Usage: /add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used")

    raw: dict[str, str] = {}
    bare_name: list[str] = []
    for token in shlex.split(text):
        if "=" in token:
            key, value = token.split("=", 1)
            raw[_normalize_key(key)] = value
        else:
            bare_name.append(token)

    name = raw.get("name") or " ".join(bare_name).strip()
    min_capacity_tb = _float(raw.get("min_tb"))
    max_capacity_tb = _float(raw.get("max_tb"))
    max_price_per_tb = _decimal(raw.get("max_eur_tb"))
    min_discount_pct = _float(raw.get("discount")) or 5.0
    cooldown_hours = int(_float(raw.get("cooldown")) or 24)

    conditions = _validated_list(raw.get("condition"), VALID_CONDITIONS, "condition")
    media_types = _validated_list(raw.get("media"), VALID_MEDIA_TYPES, "media")
    sources = _validated_list(raw.get("sources"), VALID_SOURCES, "sources")

    if min_capacity_tb is None and max_capacity_tb is None and max_price_per_tb is None and not media_types:
        raise ValueError("Ajoute au moins un filtre: min_tb, max_tb, max_eur_tb ou media.")

    if not name:
        fragments: list[str] = []
        if min_capacity_tb is not None:
            fragments.append(f">={min_capacity_tb:g} To")
        if max_price_per_tb is not None:
            fragments.append(f"<={max_price_per_tb:g} EUR/To")
        if media_types:
            fragments.append(",".join(media_types))
        name = " ".join(fragments) or "Alerte DiskCount"

    return AlertArgs(
        name=name[:120],
        min_capacity_tb=min_capacity_tb,
        max_capacity_tb=max_capacity_tb,
        conditions=conditions,
        media_types=media_types,
        sources=sources,
        max_price_per_tb=max_price_per_tb,
        min_discount_pct=min_discount_pct,
        cooldown_hours=cooldown_hours,
    )


def build_dispatcher(settings: Settings, repository: Repository, scanner: Scanner) -> Dispatcher:
    router = Router(name="diskcount")

    async def guard(message: Message) -> bool:
        if is_authorized(settings, repository, message.from_user.id if message.from_user else None):
            return True
        await message.answer("Acces refuse.")
        return False

    async def admin_guard(message: Message) -> bool:
        if is_env_admin(settings, message.from_user.id if message.from_user else None):
            return True
        await message.answer("Commande reservee a l'administrateur.")
        return False

    def current_user_id(message: Message) -> int:
        if message.from_user is None:
            raise ValueError("Telegram user is required")
        return message.from_user.id

    def include_admin_for_message(message: Message) -> bool:
        return is_env_admin(settings, message.from_user.id if message.from_user else None)

    def include_admin_for_callback(callback: CallbackQuery) -> bool:
        return is_env_admin(settings, callback.from_user.id if callback.from_user else None)

    async def send_menu(message: Message, view: str = "home", clear_keyboard: bool = False) -> None:
        include_admin = include_admin_for_message(message)
        if clear_keyboard:
            await message.answer("Navigation par tuiles activee.", reply_markup=ReplyKeyboardRemove())
        text = menu_home_text(include_admin=include_admin) if view == "home" else menu_static_text(view)
        await message.answer(text, reply_markup=build_menu_keyboard(view, include_admin=include_admin))

    async def edit_menu(callback: CallbackQuery, view: str, text: str | None = None) -> None:
        if callback.message is None:
            await callback.answer()
            return
        include_admin = include_admin_for_callback(callback)
        if text is None:
            text = menu_home_text(include_admin=include_admin) if view == "home" else menu_static_text(view)
        await callback.message.edit_text(text, reply_markup=build_menu_keyboard(view, include_admin=include_admin))
        await callback.answer()

    @router.message(CommandStart())
    async def start(message: Message) -> None:
        if not await guard(message):
            return
        repository.upsert_subscriber(message.chat.id, message.from_user.username if message.from_user else None)
        await send_menu(message, clear_keyboard=True)

    @router.message(Command("menu"))
    async def menu(message: Message) -> None:
        if not await guard(message):
            return
        await send_menu(message, clear_keyboard=True)

    @router.message(Command("help"))
    async def help_command(message: Message) -> None:
        if not await guard(message):
            return
        await send_menu(message, view="help", clear_keyboard=True)

    @router.callback_query(lambda callback: bool(callback.data and callback.data.startswith("menu:")))
    async def menu_callback(callback: CallbackQuery) -> None:
        if not is_authorized(settings, repository, callback.from_user.id if callback.from_user else None):
            await callback.answer("Acces refuse.", show_alert=True)
            return
        view = (callback.data or "menu:home").removeprefix("menu:")
        include_admin = include_admin_for_callback(callback)
        if view.startswith("admin") and not include_admin:
            await callback.answer("Commande reservee a l'administrateur.", show_alert=True)
            return
        if view == "alerts:list":
            text = format_alerts_list(repository.list_alerts(owner_user_id=callback.from_user.id))
            await edit_menu(callback, view, text)
            return
        if view == "scan:status":
            await edit_menu(callback, view, format_status(settings, repository, scanner))
            return
        if view == "scan:test":
            if callback.message is not None:
                await callback.message.edit_text(
                    "Dry-run en cours pour tes alertes...",
                    reply_markup=build_menu_keyboard("scan:test", include_admin=include_admin),
                )
            await callback.answer()
            report = await scanner.run_once(dry_run=True, target_owner_user_id=callback.from_user.id)
            if callback.message is not None:
                await callback.message.edit_text(format_scan_report(report), reply_markup=build_menu_keyboard("scan:test", include_admin=include_admin))
            return
        if view == "admin:users":
            text = format_authorized_users_list(repository.list_authorized_users(include_disabled=True))
            await edit_menu(callback, view, text)
            return
        await edit_menu(callback, view)

    @router.message(Command("users"))
    async def users(message: Message) -> None:
        if not await admin_guard(message):
            return
        rows = repository.list_authorized_users(include_disabled=True)
        if not rows:
            await message.answer("Aucun utilisateur autorise en base.", reply_markup=build_menu_keyboard("admin:users", include_admin=True))
            return
        await message.answer(format_authorized_users_list(rows), reply_markup=build_menu_keyboard("admin:users", include_admin=True))

    @router.message(Command("allow"))
    async def allow(message: Message, command: CommandObject) -> None:
        if not await admin_guard(message):
            return
        parsed = _user_id_and_label(command.args)
        if parsed is None:
            await message.answer("Usage: /allow 123456789 Nom custom")
            return
        user_id, label = parsed
        user = repository.upsert_authorized_user(user_id, label)
        await message.answer(f"Utilisateur autorise: {format_authorized_user(user)}", reply_markup=build_menu_keyboard("admin", include_admin=True))

    @router.message(Command("revoke"))
    async def revoke(message: Message, command: CommandObject) -> None:
        if not await admin_guard(message):
            return
        user_id = _user_id(command.args)
        if user_id is None:
            await message.answer("Usage: /revoke 123456789")
            return
        if not repository.revoke_authorized_user(user_id):
            await message.answer("Utilisateur introuvable.", reply_markup=build_menu_keyboard("admin:revoke", include_admin=True))
            return
        await message.answer(f"Utilisateur {user_id} desactive.", reply_markup=build_menu_keyboard("admin", include_admin=True))

    @router.message(Command("add"))
    async def add_alert(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        repository.upsert_subscriber(message.chat.id, message.from_user.username if message.from_user else None)
        try:
            args = parse_alert_args(command.args)
        except ValueError as exc:
            await message.answer(str(exc))
            return
        alert = repository.create_alert(
            chat_id=message.chat.id,
            owner_user_id=current_user_id(message),
            name=args.name,
            min_capacity_tb=args.min_capacity_tb,
            max_capacity_tb=args.max_capacity_tb,
            conditions=args.conditions,
            media_types=args.media_types,
            sources=args.sources,
            max_price_per_tb=args.max_price_per_tb,
            min_discount_pct=args.min_discount_pct,
            cooldown_hours=args.cooldown_hours,
        )
        await message.answer(f"Alerte #{alert.id} creee: {format_alert(alert)}", reply_markup=build_menu_keyboard("alerts", include_admin=include_admin_for_message(message)))

    @router.message(Command("alerts"))
    async def alerts(message: Message) -> None:
        if not await guard(message):
            return
        rows = repository.list_alerts(owner_user_id=current_user_id(message))
        await message.answer(format_alerts_list(rows), reply_markup=build_menu_keyboard("alerts:list", include_admin=include_admin_for_message(message)))

    @router.message(Command("pause"))
    async def pause(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.set_alert_enabled(current_user_id(message), alert_id, False):
            await message.answer("Alerte introuvable. Usage: /pause 1")
            return
        await message.answer(f"Alerte #{alert_id} en pause.", reply_markup=build_menu_keyboard("alerts", include_admin=include_admin_for_message(message)))

    @router.message(Command("resume"))
    async def resume(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.set_alert_enabled(current_user_id(message), alert_id, True):
            await message.answer("Alerte introuvable. Usage: /resume 1")
            return
        await message.answer(f"Alerte #{alert_id} activee.", reply_markup=build_menu_keyboard("alerts", include_admin=include_admin_for_message(message)))

    @router.message(Command("delete"))
    async def delete(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.delete_alert(current_user_id(message), alert_id):
            await message.answer("Alerte introuvable. Usage: /delete 1")
            return
        await message.answer(f"Alerte #{alert_id} supprimee.", reply_markup=build_menu_keyboard("alerts", include_admin=include_admin_for_message(message)))

    @router.message(Command("set_max_price"))
    async def set_max_price(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        parsed = _alert_id_and_price(command.args)
        if parsed is None:
            await message.answer("Usage: /set_max_price 1 20 ou /set_max_price 1 none")
            return
        alert_id, price = parsed
        if not repository.set_alert_max_price_per_tb(current_user_id(message), alert_id, price):
            await message.answer("Alerte introuvable.")
            return
        value = "desactive" if price is None else f"{price:g} EUR/To"
        await message.answer(f"Prix max de l'alerte #{alert_id}: {value}.", reply_markup=build_menu_keyboard("alerts", include_admin=include_admin_for_message(message)))

    @router.message(Command("test"))
    async def test_scan(message: Message) -> None:
        if not await guard(message):
            return
        report = await scanner.run_once(dry_run=True, target_owner_user_id=current_user_id(message))
        await message.answer(format_scan_report(report), reply_markup=build_menu_keyboard("scan:test", include_admin=include_admin_for_message(message)))

    @router.message(Command("status"))
    async def status(message: Message) -> None:
        if not await guard(message):
            return
        await message.answer(format_status(settings, repository, scanner), reply_markup=build_menu_keyboard("scan:status", include_admin=include_admin_for_message(message)))

    dispatcher = Dispatcher()
    dispatcher.include_router(router)
    return dispatcher


def format_alert(alert: Alert) -> str:
    state = "on" if alert.enabled else "off"
    parts = [
        f"#{alert.id} [{state}] {alert.name}",
        f"min={alert.min_capacity_tb:g}To" if alert.min_capacity_tb is not None else None,
        f"max={alert.max_capacity_tb:g}To" if alert.max_capacity_tb is not None else None,
        f"prix<={alert.max_price_per_tb:g}EUR/To" if alert.max_price_per_tb is not None else None,
        f"remise>={alert.min_discount_pct:g}%",
        f"etat={','.join(alert.conditions)}" if alert.conditions else None,
        f"type={','.join(alert.media_types)}" if alert.media_types else None,
        f"sources={','.join(alert.sources)}" if alert.sources else None,
    ]
    return " | ".join(part for part in parts if part)


def format_alerts_list(alerts: list[Alert]) -> str:
    if not alerts:
        return (
            "Mes alertes\n\n"
            "Aucune alerte pour ton compte.\n\n"
            "Pour en creer une:\n"
            "/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used"
        )
    return "Mes alertes\n\n" + "\n".join(format_alert(alert) for alert in alerts)


def format_status(settings: Settings, repository: Repository, scanner: Scanner) -> str:
    counts = repository.counts()
    return (
        "DiskCount status\n\n"
        f"Sources: {', '.join(source.name for source in scanner.sources)}\n"
        f"Alertes: {counts['alerts']} | Produits: {counts['products']} | "
        f"Observations: {counts['observations']} | Notifications: {counts['notifications']} | "
        f"Utilisateurs: {counts['authorized_users']}\n"
        f"Intervalle: {settings.poll_interval_seconds}s"
    )


def format_scan_report(report) -> str:
    return (
        "Dry-run termine\n\n"
        f"Offres collectees: {report.fetched}\n"
        f"Matchs: {report.matched}\n"
        f"Notifications potentielles: {report.dry_run_notifications}\n"
        f"Erreurs: {len(report.errors)}"
    )


def format_authorized_user(user) -> str:
    state = "on" if user.enabled else "off"
    role = "admin" if user.is_admin else "user"
    return f"{user.label} | {user.telegram_user_id} | {role} | {state}"


def format_authorized_users_list(users) -> str:
    if not users:
        return "Utilisateurs\n\nAucun utilisateur autorise en base."
    return "Utilisateurs\n\n" + "\n".join(format_authorized_user(user) for user in users)


def _normalize_key(key: str) -> str:
    aliases = {
        "min": "min_tb",
        "capacity_min": "min_tb",
        "min_to": "min_tb",
        "max": "max_tb",
        "capacity_max": "max_tb",
        "max_to": "max_tb",
        "price_tb": "max_eur_tb",
        "prix_to": "max_eur_tb",
        "eur_tb": "max_eur_tb",
        "eur_to": "max_eur_tb",
        "etat": "condition",
        "state": "condition",
        "type": "media",
        "media_type": "media",
        "source": "sources",
        "remise": "discount",
    }
    key = key.strip().lower().replace("-", "_")
    return aliases.get(key, key)


def _float(value: str | None) -> float | None:
    if value is None or value == "":
        return None
    return float(value.replace(",", "."))


def _decimal(value: str | None) -> Decimal | None:
    if value is None or value == "":
        return None
    return Decimal(value.replace(",", "."))


def _validated_list(value: str | None, valid: set[str], label: str) -> list[str]:
    if not value:
        return []
    items = [item.strip().lower().replace("-", "_") for item in value.split(",") if item.strip()]
    invalid = [item for item in items if item not in valid]
    if invalid:
        raise ValueError(f"Valeur {label} invalide: {', '.join(invalid)}")
    return items


def _alert_id(value: str | None) -> int | None:
    if not value:
        return None
    try:
        return int(value.strip().split()[0])
    except ValueError:
        return None


def _user_id(value: str | None) -> int | None:
    if not value:
        return None
    try:
        return int(value.strip().split()[0])
    except ValueError:
        return None


def _user_id_and_label(value: str | None) -> tuple[int, str] | None:
    if not value:
        return None
    parts = value.strip().split(maxsplit=1)
    if len(parts) != 2:
        return None
    try:
        user_id = int(parts[0])
    except ValueError:
        return None
    label = parts[1].strip()
    if not label:
        return None
    return user_id, label[:120]


def _alert_id_and_price(value: str | None) -> tuple[int, Decimal | None] | None:
    if not value:
        return None
    parts = value.strip().split()
    if len(parts) != 2:
        return None
    try:
        alert_id = int(parts[0])
    except ValueError:
        return None
    if parts[1].lower() in {"none", "off", "null", "disable", "disabled"}:
        return alert_id, None
    try:
        return alert_id, Decimal(parts[1].replace(",", "."))
    except Exception:
        return None
