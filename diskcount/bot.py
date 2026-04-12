from __future__ import annotations

import shlex
from dataclasses import dataclass
from decimal import Decimal
from typing import Any, Dict, Literal, Optional

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
VALID_DRIVE_CATEGORIES = {
    "external_3_5",
    "external_2_5",
    "internal_3_5",
    "internal_2_5",
    "internal_hybrid",
    "internal_sas",
    "external_ssd",
    "internal_ssd",
    "m2_sata",
    "m2_nvme",
    "u2_u3",
}
VALID_INTERFACES = {"sata", "sas", "nvme", "usb"}

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
    ("set_capacity", "Modifier la plage de capacite d'une alerte"),
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
    drive_categories: list[str]
    interfaces: list[str]
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


def build_alerts_keyboard(alerts: list[Alert], include_admin: bool = False) -> InlineKeyboardMarkup:
    rows: list[list[InlineKeyboardButton]] = []
    for alert in alerts[:12]:
        rows.append(
            [
                InlineKeyboardButton(text=f"Modifier #{alert.id}", callback_data=f"alert:edit:{alert.id}"),
                InlineKeyboardButton(text=f"Supprimer #{alert.id}", callback_data=f"alert:delete:{alert.id}"),
            ]
        )
    rows.extend(build_menu_keyboard("alerts:list", include_admin=include_admin).inline_keyboard)
    return InlineKeyboardMarkup(inline_keyboard=rows)


def build_alert_edit_keyboard(alert: Alert, include_admin: bool = False) -> InlineKeyboardMarkup:
    state_label = "Pauser" if alert.enabled else "Reprendre"
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text=state_label, callback_data=f"alert:enabled:{alert.id}"),
                InlineKeyboardButton(text="Supprimer", callback_data=f"alert:delete:{alert.id}"),
            ],
            [
                InlineKeyboardButton(text=_toggle_label("HDD", "rotational", alert.media_types), callback_data=f"alert:toggle:{alert.id}:media:rotational"),
                InlineKeyboardButton(text=_toggle_label("SSD", "solid_state", alert.media_types), callback_data=f"alert:toggle:{alert.id}:media:solid_state"),
            ],
            [
                InlineKeyboardButton(text=_toggle_label("New", "new", alert.conditions), callback_data=f"alert:toggle:{alert.id}:condition:new"),
                InlineKeyboardButton(text=_toggle_label("Used", "used", alert.conditions), callback_data=f"alert:toggle:{alert.id}:condition:used"),
            ],
            [
                InlineKeyboardButton(text="Stockage/Prix", callback_data=f"alert:help:{alert.id}:numbers"),
                InlineKeyboardButton(text="Categories", callback_data=f"alert:categories:{alert.id}"),
            ],
            [InlineKeyboardButton(text="Connexions", callback_data=f"alert:interfaces:{alert.id}")],
            [
                InlineKeyboardButton(text="Precedent", callback_data="menu:alerts:list"),
                InlineKeyboardButton(text="Accueil", callback_data="menu:home"),
            ],
        ]
    )


def build_alert_category_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    rows = [
        [("External 3.5", "external_3_5"), ("External 2.5", "external_2_5")],
        [("Internal 3.5", "internal_3_5"), ("Internal 2.5", "internal_2_5")],
        [("Hybrid", "internal_hybrid"), ("Internal SAS", "internal_sas")],
        [("External SSD", "external_ssd"), ("Internal SSD", "internal_ssd")],
        [("M.2 SATA", "m2_sata"), ("M.2 NVMe", "m2_nvme"), ("U.2/U.3", "u2_u3")],
    ]
    keyboard = [
        [
            InlineKeyboardButton(
                text=_toggle_label(label, value, alert.drive_categories),
                callback_data=f"alert:toggle:{alert.id}:category:{value}",
            )
            for label, value in row
        ]
        for row in rows
    ]
    keyboard.append(
        [
            InlineKeyboardButton(text="Precedent", callback_data=f"alert:edit:{alert.id}"),
            InlineKeyboardButton(text="Accueil", callback_data="menu:home"),
        ]
    )
    return InlineKeyboardMarkup(inline_keyboard=keyboard)


def build_alert_interface_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    rows = [
        [("SATA", "sata"), ("SAS", "sas")],
        [("NVMe", "nvme"), ("USB", "usb")],
    ]
    keyboard = [
        [
            InlineKeyboardButton(
                text=_toggle_label(label, value, alert.interfaces),
                callback_data=f"alert:toggle:{alert.id}:interface:{value}",
            )
            for label, value in row
        ]
        for row in rows
    ]
    keyboard.append(
        [
            InlineKeyboardButton(text="Precedent", callback_data=f"alert:edit:{alert.id}"),
            InlineKeyboardButton(text="Accueil", callback_data="menu:home"),
        ]
    )
    return InlineKeyboardMarkup(inline_keyboard=keyboard)


def _toggle_label(label: str, value: str, selected: list[str]) -> str:
    prefix = "[x]" if value in selected else "[ ]"
    return f"{prefix} {label}"


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
        "- Alertes: creer, modifier, supprimer et filtrer tes notifications.\n"
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
            "category=internal_3_5,external_3_5 interface=sata,usb "
            "discount=5 sources=diskprices,dealabs,ebay,leboncoin cooldown=24\n\n"
            "SSD en EUR/Go: max_eur_gb=0.08 media=solid_state.\n"
            "Tu peux ensuite rouvrir l'alerte depuis Mes alertes pour la modifier avec les tuiles."
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
            "max_eur_tb: prix maximum par To pour HDD.\n"
            "max_eur_gb: prix maximum par Go pour SSD, converti en EUR/To en interne.\n"
            "condition: new, used ou new,used.\n"
            "media: rotational ou solid_state.\n"
            "category: external_3_5, external_2_5, internal_3_5, internal_2_5, internal_hybrid, internal_sas, external_ssd, internal_ssd, m2_sata, m2_nvme, u2_u3.\n"
            "interface: sata, sas, nvme, usb.\n"
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
            "/set_capacity modifie la plage min/max de stockage.\n"
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
    max_price_per_gb = _decimal(raw.get("max_eur_gb"))
    if max_price_per_gb is not None:
        max_price_per_tb = max_price_per_gb * Decimal("1000")
    min_discount_pct = _float(raw.get("discount")) or 5.0
    cooldown_hours = int(_float(raw.get("cooldown")) or 24)

    conditions = _validated_list(raw.get("condition"), VALID_CONDITIONS, "condition")
    media_types = _validated_list(raw.get("media"), VALID_MEDIA_TYPES, "media")
    drive_categories = _validated_list(raw.get("category"), VALID_DRIVE_CATEGORIES, "category")
    interfaces = _validated_list(raw.get("interface"), VALID_INTERFACES, "interface")
    sources = _validated_list(raw.get("sources"), VALID_SOURCES, "sources")

    if (
        min_capacity_tb is None
        and max_capacity_tb is None
        and max_price_per_tb is None
        and not media_types
        and not drive_categories
        and not interfaces
    ):
        raise ValueError("Ajoute au moins un filtre: min_tb, max_tb, max_eur_tb, max_eur_gb, media, category ou interface.")

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
        drive_categories=drive_categories,
        interfaces=interfaces,
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
            alerts = repository.list_alerts(owner_user_id=callback.from_user.id)
            if callback.message is not None:
                await callback.message.edit_text(
                    format_alerts_list(alerts),
                    reply_markup=build_alerts_keyboard(alerts, include_admin=include_admin),
                )
            await callback.answer()
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

    @router.callback_query(lambda callback: bool(callback.data and callback.data.startswith("alert:")))
    async def alert_callback(callback: CallbackQuery) -> None:
        if not is_authorized(settings, repository, callback.from_user.id if callback.from_user else None):
            await callback.answer("Acces refuse.", show_alert=True)
            return
        parts = (callback.data or "").split(":")
        action = parts[1] if len(parts) > 1 else ""
        alert_id = _int(parts[2]) if len(parts) > 2 else None
        if alert_id is None:
            await callback.answer("Alerte invalide.", show_alert=True)
            return

        include_admin = include_admin_for_callback(callback)
        owner_user_id = callback.from_user.id
        alert = repository.get_alert(owner_user_id, alert_id)
        if alert is None:
            await callback.answer("Alerte introuvable.", show_alert=True)
            return

        if action == "delete":
            repository.delete_alert(owner_user_id, alert_id)
            alerts = repository.list_alerts(owner_user_id=owner_user_id)
            if callback.message is not None:
                await callback.message.edit_text(
                    f"Alerte #{alert_id} supprimee.\n\n{format_alerts_list(alerts)}",
                    reply_markup=build_alerts_keyboard(alerts, include_admin=include_admin),
                )
            await callback.answer()
            return

        if action == "enabled":
            repository.set_alert_enabled(owner_user_id, alert_id, not alert.enabled)
            alert = repository.get_alert(owner_user_id, alert_id)

        toggled_field: str | None = None
        if action == "toggle" and len(parts) == 5:
            field = parts[3]
            value = parts[4]
            if field not in {"condition", "media", "category", "interface"}:
                await callback.answer("Filtre invalide.", show_alert=True)
                return
            alert = repository.toggle_alert_filter_value(owner_user_id, alert_id, field, value)
            toggled_field = field

        if alert is None:
            await callback.answer("Alerte introuvable.", show_alert=True)
            return

        if toggled_field == "category":
            if callback.message is not None:
                await callback.message.edit_text(format_alert_categories_help(alert), reply_markup=build_alert_category_keyboard(alert))
            await callback.answer()
            return

        if toggled_field == "interface":
            if callback.message is not None:
                await callback.message.edit_text(format_alert_interfaces_help(alert), reply_markup=build_alert_interface_keyboard(alert))
            await callback.answer()
            return

        if action == "categories":
            if callback.message is not None:
                await callback.message.edit_text(format_alert_categories_help(alert), reply_markup=build_alert_category_keyboard(alert))
            await callback.answer()
            return

        if action == "interfaces":
            if callback.message is not None:
                await callback.message.edit_text(format_alert_interfaces_help(alert), reply_markup=build_alert_interface_keyboard(alert))
            await callback.answer()
            return

        if action == "help" and len(parts) == 4 and parts[3] == "numbers":
            if callback.message is not None:
                await callback.message.edit_text(format_alert_numbers_help(alert), reply_markup=build_alert_edit_keyboard(alert, include_admin=include_admin))
            await callback.answer()
            return

        if callback.message is not None:
            await callback.message.edit_text(format_alert_detail(alert), reply_markup=build_alert_edit_keyboard(alert, include_admin=include_admin))
        await callback.answer()

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
            drive_categories=args.drive_categories,
            interfaces=args.interfaces,
            sources=args.sources,
            max_price_per_tb=args.max_price_per_tb,
            min_discount_pct=args.min_discount_pct,
            cooldown_hours=args.cooldown_hours,
        )
        await message.answer(format_alert_detail(alert), reply_markup=build_alert_edit_keyboard(alert, include_admin=include_admin_for_message(message)))

    @router.message(Command("alerts"))
    async def alerts(message: Message) -> None:
        if not await guard(message):
            return
        rows = repository.list_alerts(owner_user_id=current_user_id(message))
        await message.answer(format_alerts_list(rows), reply_markup=build_alerts_keyboard(rows, include_admin=include_admin_for_message(message)))

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

    @router.message(Command("set_capacity"))
    async def set_capacity(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        parsed = _alert_id_and_capacity(command.args)
        if parsed is None:
            await message.answer("Usage: /set_capacity 1 16 24 ou /set_capacity 1 16 none")
            return
        alert_id, min_capacity, max_capacity = parsed
        if not repository.set_alert_capacity(current_user_id(message), alert_id, min_capacity, max_capacity):
            await message.answer("Alerte introuvable.")
            return
        await message.answer(
            f"Capacite de l'alerte #{alert_id}: min={min_capacity}To max={max_capacity}To.",
            reply_markup=build_menu_keyboard("alerts", include_admin=include_admin_for_message(message)),
        )

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

    # Interactive alert creation
    alert_creation_states: dict[int, dict[str, Any]] = {}

    def _get_alert_creation_state(user_id: int) -> dict[str, Any]:
        if user_id not in alert_creation_states:
            alert_creation_states[user_id] = {
                "step": "name",
                "name": "",
                "min_capacity_tb": None,
                "max_capacity_tb": None,
                "max_price_per_tb": None,
                "media_types": [],
                "conditions": [],
                "drive_categories": [],
                "interfaces": [],
                "sources": [],
            }
        return alert_creation_states[user_id]

    def _reset_alert_creation_state(user_id: int) -> None:
        if user_id in alert_creation_states:
            del alert_creation_states[user_id]

    @router.message(Command("create"))
    async def create_alert_command(message: Message) -> None:
        if not await guard(message):
            return
        user_id = current_user_id(message)
        _reset_alert_creation_state(user_id)
        state = _get_alert_creation_state(user_id)
        await message.answer(
            "Création d'alerte - Nom\n\n"
            "Choisis un nom pour cette alerte. Tu pourras le modifier plus tard.",
            reply_markup=InlineKeyboardMarkup(
                inline_keyboard=[
                    [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                    [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                ]
            ),
        )
        await message.answer("Nom actuel: (aucun)", parse_mode=None)

    @router.callback_query(lambda callback: bool(callback.data and callback.data.startswith("alert:create:")))
    async def alert_create_callback(callback: CallbackQuery) -> None:
        if not is_authorized(settings, repository, callback.from_user.id):
            await callback.answer("Accès refusé.", show_alert=True)
            return

        user_id = callback.from_user.id
        state = _get_alert_creation_state(user_id)
        data = callback.data.removeprefix("alert:create:")

        if data == "cancel":
            _reset_alert_creation_state(user_id)
            await edit_menu(callback, "alerts:list")
            await callback.answer()
            return

        if data == "next":
            current_step = state["step"]
            if current_step == "name":
                state["step"] = "min_capacity"
                await callback.message.edit_text(
                    "Création d'alerte - Capacité minimale (To)\n\n"
                    "Définis la capacité minimale en To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10", callback_data="alert:create:set:min_capacity:-10")],
                            [InlineKeyboardButton(text="-5", callback_data="alert:create:set:min_capacity:-5")],
                            [InlineKeyboardButton(text="-1", callback_data="alert:create:set:min_capacity:-1")],
                            [InlineKeyboardButton(text=f"{state.get('min_capacity_tb', 0):g} To", callback_data="alert:create:edit:min_capacity")],
                            [InlineKeyboardButton(text="+1", callback_data="alert:create:set:min_capacity:+1")],
                            [InlineKeyboardButton(text="+5", callback_data="alert:create:set:min_capacity:+5")],
                            [InlineKeyboardButton(text="+10", callback_data="alert:create:set:min_capacity:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:min_capacity:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "min_capacity":
                state["step"] = "max_capacity"
                await callback.message.edit_text(
                    "Création d'alerte - Capacité maximale (To)\n\n"
                    "Définis la capacité maximale en To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10", callback_data="alert:create:set:max_capacity:-10")],
                            [InlineKeyboardButton(text="-5", callback_data="alert:create:set:max_capacity:-5")],
                            [InlineKeyboardButton(text="-1", callback_data="alert:create:set:max_capacity:-1")],
                            [InlineKeyboardButton(text=f"{state.get('max_capacity_tb', 0):g} To", callback_data="alert:create:edit:max_capacity")],
                            [InlineKeyboardButton(text="+1", callback_data="alert:create:set:max_capacity:+1")],
                            [InlineKeyboardButton(text="+5", callback_data="alert:create:set:max_capacity:+5")],
                            [InlineKeyboardButton(text="+10", callback_data="alert:create:set:max_capacity:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:max_capacity:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "max_capacity":
                state["step"] = "max_price"
                await callback.message.edit_text(
                    "Création d'alerte - Prix maximal (€/To)\n\n"
                    "Définis le prix maximal par To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10 €/To", callback_data="alert:create:set:max_price:-10")],
                            [InlineKeyboardButton(text="-5 €/To", callback_data="alert:create:set:max_price:-5")],
                            [InlineKeyboardButton(text="-1 €/To", callback_data="alert:create:set:max_price:-1")],
                            [InlineKeyboardButton(text=f"{state.get('max_price_per_tb', 0):g} €/To", callback_data="alert:create:edit:max_price")],
                            [InlineKeyboardButton(text="+1 €/To", callback_data="alert:create:set:max_price:+1")],
                            [InlineKeyboardButton(text="+5 €/To", callback_data="alert:create:set:max_price:+5")],
                            [InlineKeyboardButton(text="+10 €/To", callback_data="alert:create:set:max_price:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:max_price:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "max_price":
                state["step"] = "media"
                await callback.message.edit_text(
                    "Création d'alerte - Type de média\n\n"
                    "Sélectionne le ou les types de média.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("HDD", "rotational", state["media_types"]),
                                    callback_data="alert:create:toggle:media:rotational"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("SSD", "solid_state", state["media_types"]),
                                    callback_data="alert:create:toggle:media:solid_state"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "media":
                state["step"] = "condition"
                await callback.message.edit_text(
                    "Création d'alerte - État\n\n"
                    "Sélectionne le ou les états du produit.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Neuf", "new", state["conditions"]),
                                    callback_data="alert:create:toggle:condition:new"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("Usagé", "used", state["conditions"]),
                                    callback_data="alert:create:toggle:condition:used"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "condition":
                state["step"] = "categories"
                await callback.message.edit_text(
                    "Création d'alerte - Catégories DiskPrices\n\n"
                    "Sélectionne les catégories DiskPrices.",
                    reply_markup=build_alert_category_keyboard_for_creation(state),
                )
            elif current_step == "categories":
                state["step"] = "interfaces"
                await callback.message.edit_text(
                    "Création d'alerte - Interfaces\n\n"
                    "Sélectionne les interfaces supportées.",
                    reply_markup=build_alert_interface_keyboard_for_creation(state),
                )
            elif current_step == "interfaces":
                state["step"] = "sources"
                await callback.message.edit_text(
                    "Création d'alerte - Sources\n\n"
                    "Sélectionne les sources à surveiller.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("DiskPrices", "diskprices", state["sources"]),
                                    callback_data="alert:create:toggle:sources:diskprices"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("Dealabs", "dealabs", state["sources"]),
                                    callback_data="alert:create:toggle:sources:dealabs"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("eBay", "ebay", state["sources"]),
                                    callback_data="alert:create:toggle:sources:ebay"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("leboncoin", "leboncoin", state["sources"]),
                                    callback_data="alert:create:toggle:sources:leboncoin"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Idealo", "idealo", state["sources"]),
                                    callback_data="alert:create:toggle:sources:idealo"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("leDenicheur", "ledenicheur", state["sources"]),
                                    callback_data="alert:create:toggle:sources:ledenicheur"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Keepa", "keepa", state["sources"]),
                                    callback_data="alert:create:toggle:sources:keepa"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "sources":
                state["step"] = "confirm"
                await callback.message.edit_text(
                    "Création d'alerte - Récapitulatif et confirmation\n\n"
                    f"Nom: {state['name'] or '(aucun)'}\n"
                    f"Capacité min: {state['min_capacity_tb'] or 'Aucune'}\n"
                    f"Capacité max: {state['max_capacity_tb'] or 'Aucune'}\n"
                    f"Prix max (€/To): {state['max_price_per_tb'] or 'Aucune'}\n"
                    f"États: {', '.join(state['conditions']) if state['conditions'] else 'Aucun'}\n"
                    f"Types: {', '.join(state['media_types']) if state['media_types'] else 'Aucun'}\n"
                    f"Catégories: {', '.join(state['drive_categories']) if state['drive_categories'] else 'Aucune'}\n"
                    f"Interfaces: {', '.join(state['interfaces']) if state['interfaces'] else 'Aucune'}\n"
                    f"Sources: {', '.join(state['sources']) if state['sources'] else 'Aucune'}\n\n"
                    "Créer cette alerte ?",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="Créer", callback_data="alert:create:confirm")],
                            [InlineKeyboardButton(text="Précédent", callback_data="alert:create:prev")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            await callback.answer()
            return

        if data == "prev":
            state = _get_alert_creation_state(user_id)
            current_step = state["step"]
            if current_step == "min_capacity":
                state["step"] = "name"
                await callback.message.edit_text(
                    "Création d'alerte - Nom\n\n"
                    "Choisis un nom pour cette alerte. Tu pourras le modifier plus tard.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "max_capacity":
                state["step"] = "min_capacity"
                await callback.message.edit_text(
                    "Création d'alerte - Capacité minimale (To)\n\n"
                    "Définis la capacité minimale en To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10", callback_data="alert:create:set:min_capacity:-10")],
                            [InlineKeyboardButton(text="-5", callback_data="alert:create:set:min_capacity:-5")],
                            [InlineKeyboardButton(text="-1", callback_data="alert:create:set:min_capacity:-1")],
                            [InlineKeyboardButton(text=f"{state.get('min_capacity_tb', 0):g} To", callback_data="alert:create:edit:min_capacity")],
                            [InlineKeyboardButton(text="+1", callback_data="alert:create:set:min_capacity:+1")],
                            [InlineKeyboardButton(text="+5", callback_data="alert:create:set:min_capacity:+5")],
                            [InlineKeyboardButton(text="+10", callback_data="alert:create:set:min_capacity:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:min_capacity:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "max_price":
                state["step"] = "max_capacity"
                await callback.message.edit_text(
                    "Création d'alerte - Capacité maximale (To)\n\n"
                    "Définis la capacité maximale en To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10", callback_data="alert:create:set:max_capacity:-10")],
                            [InlineKeyboardButton(text="-5", callback_data="alert:create:set:max_capacity:-5")],
                            [InlineKeyboardButton(text="-1", callback_data="alert:create:set:max_capacity:-1")],
                            [InlineKeyboardButton(text=f"{state.get('max_capacity_tb', 0):g} To", callback_data="alert:create:edit:max_capacity")],
                            [InlineKeyboardButton(text="+1", callback_data="alert:create:set:max_capacity:+1")],
                            [InlineKeyboardButton(text="+5", callback_data="alert:create:set:max_capacity:+5")],
                            [InlineKeyboardButton(text="+10", callback_data="alert:create:set:max_capacity:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:max_capacity:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "media":
                state["step"] = "max_price"
                await callback.message.edit_text(
                    "Création d'alerte - Prix maximal (€/To)\n\n"
                    "Définis le prix maximal par To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10 €/To", callback_data="alert:create:set:max_price:-10")],
                            [InlineKeyboardButton(text="-5 €/To", callback_data="alert:create:set:max_price:-5")],
                            [InlineKeyboardButton(text="-1 €/To", callback_data="alert:create:set:max_price:-1")],
                            [InlineKeyboardButton(text=f"{state.get('max_price_per_tb', 0):g} €/To", callback_data="alert:create:edit:max_price")],
                            [InlineKeyboardButton(text="+1 €/To", callback_data="alert:create:set:max_price:+1")],
                            [InlineKeyboardButton(text="+5 €/To", callback_data="alert:create:set:max_price:+5")],
                            [InlineKeyboardButton(text="+10 €/To", callback_data="alert:create:set:max_price:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:max_price:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "condition":
                state["step"] = "media"
                await callback.message.edit_text(
                    "Création d'alerte - Type de média\n\n"
                    "Sélectionne le ou les types de média.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("HDD", "rotational", state["media_types"]),
                                    callback_data="alert:create:toggle:media:rotational"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("SSD", "solid_state", state["media_types"]),
                                    callback_data="alert:create:toggle:media:solid_state"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "categories":
                state["step"] = "condition"
                await callback.message.edit_text(
                    "Création d'alerte - État\n\n"
                    "Sélectionne le ou les états du produit.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Neuf", "new", state["conditions"]),
                                    callback_data="alert:create:toggle:condition:new"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("Usagé", "used", state["conditions"]),
                                    callback_data="alert:create:toggle:condition:used"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "interfaces":
                state["step"] = "categories"
                await callback.message.edit_text(
                    "Création d'alerte - Catégories DiskPrices\n\n"
                    "Sélectionne les catégories DiskPrices.",
                    reply_markup=build_alert_category_keyboard_for_creation(state),
                )
            elif current_step == "sources":
                state["step"] = "interfaces"
                await callback.message.edit_text(
                    "Création d'alerte - Interfaces\n\n"
                    "Sélectionne les interfaces supportées.",
                    reply_markup=build_alert_interface_keyboard_for_creation(state),
                )
            elif current_step == "confirm":
                state["step"] = "sources"
                await callback.message.edit_text(
                    "Création d'alerte - Sources\n\n"
                    "Sélectionne les sources à surveiller.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("DiskPrices", "diskprices", state["sources"]),
                                    callback_data="alert:create:toggle:sources:diskprices"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("Dealabs", "dealabs", state["sources"]),
                                    callback_data="alert:create:toggle:sources:dealabs"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("eBay", "ebay", state["sources"]),
                                    callback_data="alert:create:toggle:sources:ebay"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("leboncoin", "leboncoin", state["sources"]),
                                    callback_data="alert:create:toggle:sources:leboncoin"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Idealo", "idealo", state["sources"]),
                                    callback_data="alert:create:toggle:sources:idealo"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("leDenicheur", "ledenicheur", state["sources"]),
                                    callback_data="alert:create:toggle:sources:ledenicheur"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Keepa", "keepa", state["sources"]),
                                    callback_data="alert:create:toggle:sources:keepa"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            await callback.answer()
            return

        if data.startswith("set:"):
            _, field, value = data.split(":", 2)
            state = _get_alert_creation_state(user_id)

            if field == "min_capacity":
                if value == "none":
                    state["min_capacity_tb"] = None
                else:
                    try:
                        delta = float(value)
                        current = state.get("min_capacity_tb", 0.0)
                        state["min_capacity_tb"] = max(0.0, current + delta) if current else delta
                    except:
                        pass

            elif field == "max_capacity":
                if value == "none":
                    state["max_capacity_tb"] = None
                else:
                    try:
                        delta = float(value)
                        current = state.get("max_capacity_tb", 0.0)
                        state["max_capacity_tb"] = max(0.0, current + delta) if current else delta
                    except:
                        pass

            elif field == "max_price":
                if value == "none":
                    state["max_price_per_tb"] = None
                else:
                    try:
                        delta = float(value)
                        current = state.get("max_price_per_tb", 0.0)
                        if current:
                            state["max_price_per_tb"] = max(0.0, current + delta)
                        else:
                            state["max_price_per_tb"] = delta if delta > 0 else None
                    except:
                        pass

            current_step = state["step"]
            if current_step == "min_capacity":
                await callback.message.edit_text(
                    "Création d'alerte - Capacité minimale (To)\n\n"
                    "Définis la capacité minimale en To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10", callback_data="alert:create:set:min_capacity:-10")],
                            [InlineKeyboardButton(text="-5", callback_data="alert:create:set:min_capacity:-5")],
                            [InlineKeyboardButton(text="-1", callback_data="alert:create:set:min_capacity:-1")],
                            [InlineKeyboardButton(text=f"{state.get('min_capacity_tb', 0):g} To", callback_data="alert:create:edit:min_capacity")],
                            [InlineKeyboardButton(text="+1", callback_data="alert:create:set:min_capacity:+1")],
                            [InlineKeyboardButton(text="+5", callback_data="alert:create:set:min_capacity:+5")],
                            [InlineKeyboardButton(text="+10", callback_data="alert:create:set:min_capacity:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:min_capacity:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "max_capacity":
                await callback.message.edit_text(
                    "Création d'alerte - Capacité maximale (To)\n\n"
                    "Définis la capacité maximale en To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10", callback_data="alert:create:set:max_capacity:-10")],
                            [InlineKeyboardButton(text="-5", callback_data="alert:create:set:max_capacity:-5")],
                            [InlineKeyboardButton(text="-1", callback_data="alert:create:set:max_capacity:-1")],
                            [InlineKeyboardButton(text=f"{state.get('max_capacity_tb', 0):g} To", callback_data="alert:create:edit:max_capacity")],
                            [InlineKeyboardButton(text="+1", callback_data="alert:create:set:max_capacity:+1")],
                            [InlineKeyboardButton(text="+5", callback_data="alert:create:set:max_capacity:+5")],
                            [InlineKeyboardButton(text="+10", callback_data="alert:create:set:max_capacity:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:max_capacity:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "max_price":
                await callback.message.edit_text(
                    "Création d'alerte - Prix maximal (€/To)\n\n"
                    "Définis le prix maximal par To. Tu peux utiliser les boutons pour ajuster.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [InlineKeyboardButton(text="-10 €/To", callback_data="alert:create:set:max_price:-10")],
                            [InlineKeyboardButton(text="-5 €/To", callback_data="alert:create:set:max_price:-5")],
                            [InlineKeyboardButton(text="-1 €/To", callback_data="alert:create:set:max_price:-1")],
                            [InlineKeyboardButton(text=f"{state.get('max_price_per_tb', 0):g} €/To", callback_data="alert:create:edit:max_price")],
                            [InlineKeyboardButton(text="+1 €/To", callback_data="alert:create:set:max_price:+1")],
                            [InlineKeyboardButton(text="+5 €/To", callback_data="alert:create:set:max_price:+5")],
                            [InlineKeyboardButton(text="+10 €/To", callback_data="alert:create:set:max_price:+10")],
                            [InlineKeyboardButton(text="Aucune limite", callback_data="alert:create:set:max_price:none")],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            await callback.answer()
            return

        if data.startswith("toggle:"):
            _, field, value = data.split(":", 2)
            state = _get_alert_creation_state(user_id)

            if field == "media":
                if value in state["media_types"]:
                    state["media_types"].remove(value)
                else:
                    state["media_types"].append(value)

            elif field == "condition":
                if value in state["conditions"]:
                    state["conditions"].remove(value)
                else:
                    state["conditions"].append(value)

            elif field == "category":
                if value in state["drive_categories"]:
                    state["drive_categories"].remove(value)
                else:
                    state["drive_categories"].append(value)

            elif field == "interface":
                if value in state["interfaces"]:
                    state["interfaces"].remove(value)
                else:
                    state["interfaces"].append(value)

            elif field == "sources":
                if value in state["sources"]:
                    state["sources"].remove(value)
                else:
                    state["sources"].append(value)

            current_step = state["step"]
            if current_step == "media":
                await callback.message.edit_text(
                    "Création d'alerte - Type de média\n\n"
                    "Sélectionne le ou les types de média.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("HDD", "rotational", state["media_types"]),
                                    callback_data="alert:create:toggle:media:rotational"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("SSD", "solid_state", state["media_types"]),
                                    callback_data="alert:create:toggle:media:solid_state"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "condition":
                await callback.message.edit_text(
                    "Création d'alerte - État\n\n"
                    "Sélectionne le ou les états du produit.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Neuf", "new", state["conditions"]),
                                    callback_data="alert:create:toggle:condition:new"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("Usagé", "used", state["conditions"]),
                                    callback_data="alert:create:toggle:condition:used"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            elif current_step == "categories":
                await callback.message.edit_text(
                    "Création d'alerte - Catégories DiskPrices\n\n"
                    "Sélectionne les catégories DiskPrices.",
                    reply_markup=build_alert_category_keyboard_for_creation(state),
                )
            elif current_step == "interfaces":
                await callback.message.edit_text(
                    "Création d'alerte - Interfaces\n\n"
                    "Sélectionne les interfaces supportées.",
                    reply_markup=build_alert_interface_keyboard_for_creation(state),
                )
            elif current_step == "sources":
                await callback.message.edit_text(
                    "Création d'alerte - Sources\n\n"
                    "Sélectionne les sources à surveiller.",
                    reply_markup=InlineKeyboardMarkup(
                        inline_keyboard=[
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("DiskPrices", "diskprices", state["sources"]),
                                    callback_data="alert:create:toggle:sources:diskprices"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("Dealabs", "dealabs", state["sources"]),
                                    callback_data="alert:create:toggle:sources:dealabs"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("eBay", "ebay", state["sources"]),
                                    callback_data="alert:create:toggle:sources:ebay"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("leboncoin", "leboncoin", state["sources"]),
                                    callback_data="alert:create:toggle:sources:leboncoin"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Idealo", "idealo", state["sources"]),
                                    callback_data="alert:create:toggle:sources:idealo"
                                ),
                                InlineKeyboardButton(
                                    text=_toggle_label("leDenicheur", "ledenicheur", state["sources"]),
                                    callback_data="alert:create:toggle:sources:ledenicheur"
                                ),
                            ],
                            [
                                InlineKeyboardButton(
                                    text=_toggle_label("Keepa", "keepa", state["sources"]),
                                    callback_data="alert:create:toggle:sources:keepa"
                                ),
                            ],
                            [InlineKeyboardButton(text="Suivant", callback_data="alert:create:next")],
                            [InlineKeyboardButton(text="Annuler", callback_data="alert:create:cancel")],
                        ]
                    ),
                )
            await callback.answer()
            return

        if data == "confirm":
            state = _get_alert_creation_state(user_id)
            try:
                alert_args = AlertArgs(
                    name=state.get("name", "Alerte DiskCount"),
                    min_capacity_tb=state.get("min_capacity_tb"),
                    max_capacity_tb=state.get("max_capacity_tb"),
                    max_price_per_tb=state.get("max_price_per_tb"),
                    conditions=state.get("conditions", []),
                    media_types=state.get("media_types", []),
                    drive_categories=state.get("drive_categories", []),
                    interfaces=state.get("interfaces", []),
                    sources=state.get("sources", []),
                    min_discount_pct=5.0,
                    cooldown_hours=24,
                )

                from .parsing import parse_alert_args
                # Note: parse_alert_args expects a string, but we have AlertArgs
                # We need to convert AlertArgs back to string format or create alert directly

                # For simplicity, we'll create the alert directly using repository
                from .db import Alert as DBAlert
                from decimal import Decimal

                alert = DBAlert(
                    name=alert_args.name,
                    min_capacity_tb=alert_args.min_capacity_tb,
                    max_capacity_tb=alert_args.max_capacity_tb,
                    max_price_per_tb=float(alert_args.max_price_per_tb) if alert_args.max_price_per_tb else None,
                    conditions=alert_args.conditions,
                    media_types=alert_args.media_types,
                    drive_categories=alert_args.drive_categories,
                    interfaces=alert_args.interfaces,
                    sources=alert_args.sources,
                    min_discount_pct=alert_args.min_discount_pct,
                    cooldown_hours=alert_args.cooldown_hours,
                    owner_user_id=user_id,
                )

                repository.upsert_alert(alert)

                _reset_alert_creation_state(user_id)

                await callback.message.edit_text(
                    f"✅ Alerte créée avec succès !\n\n"
                    f"Nom: {alert_args.name}\n"
                    f"ID: {alert.id}",
                    reply_markup=build_menu_keyboard("alerts:list"),
                )

            except Exception as e:
                await callback.message.edit_text(
                    f"❌ Erreur lors de la création de l'alerte: {str(e)}\n\n"
                    "Veuillez réessayer.",
                    reply_markup=build_menu_keyboard("alerts:list"),
                )
                _reset_alert_creation_state(user_id)

            await callback.answer()
            return

    # End of interactive alert creation handlers

    dispatcher = Dispatcher()
    dispatcher.include_router(router)
    return dispatcher


def format_alert(alert: Alert) -> str:
    state = "on" if alert.enabled else "off"
    parts = [
        f"#{alert.id} [{state}] {alert.name}",
        f"min={alert.min_capacity_tb:g}To" if alert.min_capacity_tb is not None else None,
        f"max={alert.max_capacity_tb:g}To" if alert.max_capacity_tb is not None else None,
        format_price_limit(alert),
        f"remise>={alert.min_discount_pct:g}%",
        f"etat={','.join(alert.conditions)}" if alert.conditions else None,
        f"type={','.join(alert.media_types)}" if alert.media_types else None,
        f"cat={','.join(alert.drive_categories)}" if alert.drive_categories else None,
        f"conn={','.join(alert.interfaces)}" if alert.interfaces else None,
        f"sources={','.join(alert.sources)}" if alert.sources else None,
    ]
    return " | ".join(part for part in parts if part)


def format_price_limit(alert: Alert) -> str | None:
    if alert.max_price_per_tb is None:
        return None
    price = Decimal(alert.max_price_per_tb)
    if alert.media_types == ["solid_state"]:
        return f"prix<={price / Decimal('1000'):g}EUR/Go"
    return f"prix<={price:g}EUR/To"


def format_alerts_list(alerts: list[Alert]) -> str:
    if not alerts:
        return (
            "Mes alertes\n\n"
            "Aucune alerte pour ton compte.\n\n"
            "Pour en creer une:\n"
            "/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used"
        )
    return "Mes alertes\n\n" + "\n".join(format_alert(alert) for alert in alerts)


def format_alert_detail(alert: Alert) -> str:
    return (
        "Modifier une notification\n\n"
        f"{format_alert(alert)}\n\n"
        "Utilise les cases pour HDD/SSD, New/Used, categories DiskPrices et connexions.\n"
        "Pour la capacite et le prix, ouvre Stockage/Prix puis envoie la commande indiquee."
    )


def format_alert_numbers_help(alert: Alert) -> str:
    return (
        "Stockage et prix\n\n"
        f"Alerte #{alert.id}\n"
        "Capacite: /set_capacity {id} 16 24 ou /set_capacity {id} 16 none\n"
        "Prix HDD: /set_max_price {id} 20\n"
        "Prix SSD: /set_max_price {id} 0.08 gb\n\n"
        "Le bot stocke le seuil en EUR/To. Pour SSD, le prix EUR/Go est converti automatiquement."
    ).format(id=alert.id)


def format_alert_categories_help(alert: Alert) -> str:
    return (
        "Categories DiskPrices\n\n"
        f"Alerte #{alert.id}\n"
        "Coche les familles voulues: external/internal 3.5, 2.5, Hybrid, Internal SAS, "
        "External/Internal SSD, M.2 SATA, M.2 NVMe, U.2/U.3."
    )


def format_alert_interfaces_help(alert: Alert) -> str:
    return (
        "Connexions\n\n"
        f"Alerte #{alert.id}\n"
        "Coche les connexions voulues: SATA, SAS, NVMe ou USB. Le bot les deduit du titre et des champs DiskPrices."
    )


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
        "price_gb": "max_eur_gb",
        "prix_go": "max_eur_gb",
        "eur_gb": "max_eur_gb",
        "eur_go": "max_eur_gb",
        "etat": "condition",
        "state": "condition",
        "type": "media",
        "media_type": "media",
        "categories": "category",
        "drive_category": "category",
        "form": "category",
        "form_factor": "category",
        "interfaces": "interface",
        "connection": "interface",
        "connectique": "interface",
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
    return _int(value.strip().split()[0])


def _int(value: str | None) -> int | None:
    if not value:
        return None
    try:
        return int(value)
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
    if len(parts) not in (2, 3):
        return None
    try:
        alert_id = int(parts[0])
    except ValueError:
        return None
    if parts[1].lower() in {"none", "off", "null", "disable", "disabled"}:
        return alert_id, None
    try:
        price = Decimal(parts[1].replace(",", "."))
        if len(parts) == 3 and parts[2].lower() in {"gb", "go"}:
            price = price * Decimal("1000")
        return alert_id, price
    except Exception:
        return None


def _alert_id_and_capacity(value: str | None) -> tuple[int, float | None, float | None] | None:
    if not value:
        return None
    parts = value.strip().split()
    if len(parts) != 3:
        return None
    try:
        alert_id = int(parts[0])
    except ValueError:
        return None
    try:
        min_capacity = None if parts[1].lower() in {"none", "off", "null"} else float(parts[1].replace(",", "."))
        max_capacity = None if parts[2].lower() in {"none", "off", "null"} else float(parts[2].replace(",", "."))
    except ValueError:
        return None
    return alert_id, min_capacity, max_capacity
