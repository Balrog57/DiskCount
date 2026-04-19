from __future__ import annotations

import shlex
import time
from dataclasses import dataclass, field
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
from .presets import CAPACITY_PRESETS, HDD_CAPACITY_KEYS, SSD_CAPACITY_KEYS
from .scanner import Scanner

VALID_CONDITIONS = {"new", "used"}
VALID_MEDIA_TYPES = {"rotational", "solid_state"}
VALID_SOURCES = {
    "diskprices",
    "pricepergig",
    "pricepertb",
    "dealabs",
    "idealo",
    "ledenicheur",
    "leboncoin",
    "ebay",
    "keepa",
}
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
DRAFT_TTL_SECONDS = 3600

HDD_CATEGORIES: tuple[tuple[str, str], ...] = (
    ("External 3.5", "external_3_5"),
    ("External 2.5", "external_2_5"),
    ("Internal 3.5", "internal_3_5"),
    ("Internal 2.5", "internal_2_5"),
    ("Internal Hybrid", "internal_hybrid"),
    ("Internal SAS", "internal_sas"),
)
SSD_CATEGORIES: tuple[tuple[str, str], ...] = (
    ("External SSD", "external_ssd"),
    ("Internal SSD", "internal_ssd"),
    ("M.2 SATA", "m2_sata"),
    ("M.2 NVMe", "m2_nvme"),
    ("U.2/U.3", "u2_u3"),
)
INTERFACE_OPTIONS: tuple[tuple[str, str], ...] = (("SATA", "sata"), ("SAS", "sas"), ("NVMe", "nvme"), ("USB", "usb"))
SOURCE_OPTIONS: tuple[tuple[str, str], ...] = (
    ("DiskPrices", "diskprices"),
    ("PricePerGig", "pricepergig"),
    ("PricePerTB", "pricepertb"),
    ("Dealabs", "dealabs"),
    ("eBay", "ebay"),
    ("leboncoin", "leboncoin"),
    ("Idealo", "idealo"),
    ("leDenicheur", "ledenicheur"),
    ("Keepa", "keepa"),
)
PRICE_PRESETS: dict[str, tuple[str, Decimal | None, str]] = {
    "none": ("Aucune limite", None, "all"),
    "h15": ("HDD <=15 EUR/To", Decimal("15"), "rotational"),
    "h18": ("HDD <=18 EUR/To", Decimal("18"), "rotational"),
    "h20": ("HDD <=20 EUR/To", Decimal("20"), "rotational"),
    "h22": ("HDD <=22 EUR/To", Decimal("22"), "rotational"),
    "h25": ("HDD <=25 EUR/To", Decimal("25"), "rotational"),
    "s004": ("SSD <=0.04 EUR/Go", Decimal("40"), "solid_state"),
    "s006": ("SSD <=0.06 EUR/Go", Decimal("60"), "solid_state"),
    "s008": ("SSD <=0.08 EUR/Go", Decimal("80"), "solid_state"),
    "s010": ("SSD <=0.10 EUR/Go", Decimal("100"), "solid_state"),
    "s012": ("SSD <=0.12 EUR/Go", Decimal("120"), "solid_state"),
}
HDD_PRICE_KEYS = ("h15", "h18", "h20", "h22", "h25")
SSD_PRICE_KEYS = ("s004", "s006", "s008", "s010", "s012")
WIZARD_STEPS = ("media", "condition", "capacity", "price", "categories", "interfaces", "confirm")

USER_COMMANDS: tuple[tuple[str, str], ...] = (
    ("start", "Demarrer le bot et enregistrer le chat"),
    ("menu", "Ouvrir la navigation par tuiles"),
    ("create", "Creer une alerte avec les tuiles"),
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
    capacity_presets: list[str]
    conditions: list[str]
    media_types: list[str]
    drive_categories: list[str]
    interfaces: list[str]
    sources: list[str]
    max_price_per_tb: Decimal | None
    min_discount_pct: float
    cooldown_hours: int


@dataclass
class AlertDraft:
    step: str = "media"
    name: str = "Alerte DiskCount"
    min_capacity_tb: float | None = None
    max_capacity_tb: float | None = None
    capacity_presets: list[str] = field(default_factory=lambda: ["hdd_16_20"])
    max_price_per_tb: Decimal | None = Decimal("20")
    conditions: list[str] = field(default_factory=lambda: ["new", "used"])
    media_types: list[str] = field(default_factory=lambda: ["rotational"])
    drive_categories: list[str] = field(default_factory=list)
    interfaces: list[str] = field(default_factory=list)
    sources: list[str] = field(default_factory=list)
    updated_at: float = field(default_factory=time.time)

    def touch(self) -> None:
        self.updated_at = time.time()


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
            [button("Creer une alerte", "draft:start")],
            [button("Mes alertes", "menu:alerts:list"), button("Scanner/Test", "menu:scan")],
            [button("Aide", "menu:help")],
        ],
        "alerts": [
            [button("Creer une alerte", "draft:start")],
            [button("Mes alertes", "menu:alerts:list")],
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
            [button("DiskPrices", "menu:sources:diskprices"), button("PricePerGig", "menu:sources:pricepergig")],
            [button("PricePerTB", "menu:sources:pricepertb"), button("Dealabs", "menu:sources:dealabs")],
            [button("eBay", "menu:sources:ebay"), button("leboncoin", "menu:sources:leboncoin")],
            [button("Idealo", "menu:sources:idealo"), button("leDenicheur", "menu:sources:ledenicheur")],
            [button("Keepa", "menu:sources:keepa")],
            *nav,
        ],
        "sources:diskprices": nav,
        "sources:pricepergig": nav,
        "sources:pricepertb": nav,
        "sources:dealabs": nav,
        "sources:ebay": nav,
        "sources:leboncoin": nav,
        "sources:idealo": nav,
        "sources:ledenicheur": nav,
        "sources:keepa": nav,
        "help": [
            [button("Creer une alerte", "menu:help:create"), button("Mes alertes", "menu:help:alerts")],
            [button("Capacites", "menu:help:capacity"), button("Prix", "menu:help:price")],
            [button("Categories", "menu:help:categories"), button("Connexions", "menu:help:interfaces")],
            [button("Scanner/Test", "menu:help:scan"), button("Admin", "menu:help:admin")],
            [button("Sources backend", "menu:help:sources"), button("Commandes", "menu:help:commands")],
            [button("Fallback /add", "menu:alerts:add"), button("Filtres texte", "menu:help:filters")],
            *nav,
        ],
        "help:create": nav,
        "help:alerts": nav,
        "help:capacity": nav,
        "help:price": nav,
        "help:categories": nav,
        "help:interfaces": nav,
        "help:scan": nav,
        "help:admin": nav,
        "help:sources": nav,
        "help:filters": nav,
        "help:commands": nav,
        "admin": [
            [button("Utilisateurs", "admin:list")],
            [button("Ajouter", "admin:add"), button("Revoquer", "admin:revoke")],
            [button("Reactiver", "admin:reactivate")],
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
        rows.append([InlineKeyboardButton(text=format_alert_button(alert), callback_data=f"alert:edit:{alert.id}")])
    rows.append([InlineKeyboardButton(text="Creer une alerte", callback_data="draft:start")])
    rows.extend(build_menu_keyboard("alerts:list", include_admin=include_admin).inline_keyboard)
    return InlineKeyboardMarkup(inline_keyboard=rows)


def build_alert_edit_keyboard(alert: Alert, include_admin: bool = False) -> InlineKeyboardMarkup:
    state_label = "Pauser" if alert.enabled else "Reprendre"
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [InlineKeyboardButton(text=state_label, callback_data=f"alert:enabled:{alert.id}")],
            [
                InlineKeyboardButton(text="Type", callback_data=f"alert:media:{alert.id}"),
                InlineKeyboardButton(text="Etat", callback_data=f"alert:condition:{alert.id}"),
            ],
            [
                InlineKeyboardButton(text="Capacite", callback_data=f"alert:capacity:{alert.id}"),
                InlineKeyboardButton(text="Prix", callback_data=f"alert:price:{alert.id}"),
            ],
            [
                InlineKeyboardButton(text="Categories", callback_data=f"alert:categories:{alert.id}"),
                InlineKeyboardButton(text="Connexions", callback_data=f"alert:interfaces:{alert.id}"),
            ],
            [InlineKeyboardButton(text="Supprimer", callback_data=f"alert:delete:{alert.id}")],
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


def build_alert_media_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text=_toggle_label("HDD", "rotational", alert.media_types), callback_data=f"alert:toggle:{alert.id}:media:rotational"),
                InlineKeyboardButton(text=_toggle_label("SSD", "solid_state", alert.media_types), callback_data=f"alert:toggle:{alert.id}:media:solid_state"),
            ],
            _alert_nav(alert.id),
        ]
    )


def build_alert_condition_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text=_toggle_label("New", "new", alert.conditions), callback_data=f"alert:toggle:{alert.id}:condition:new"),
                InlineKeyboardButton(text=_toggle_label("Used", "used", alert.conditions), callback_data=f"alert:toggle:{alert.id}:condition:used"),
            ],
            _alert_nav(alert.id),
        ]
    )


def build_alert_capacity_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    selected = selected_capacity_keys(alert)
    all_label = _toggle_label(CAPACITY_PRESETS["all"][0], "all", ["all"] if not selected and alert.min_capacity_tb is None and alert.max_capacity_tb is None else [])
    rows = [[InlineKeyboardButton(text=all_label, callback_data=f"alert:cap:{alert.id}:all")]]
    rows.extend(_preset_rows("alert:cap", alert.id, HDD_CAPACITY_KEYS, selected))
    rows.extend(_preset_rows("alert:cap", alert.id, SSD_CAPACITY_KEYS, selected))
    rows.append(_alert_nav(alert.id))
    return InlineKeyboardMarkup(inline_keyboard=rows)


def build_alert_price_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    rows = [[InlineKeyboardButton(text=PRICE_PRESETS["none"][0], callback_data=f"alert:price_set:{alert.id}:none")]]
    rows.extend(_price_rows("alert:price_set", alert.id, HDD_PRICE_KEYS, selected_price_key(alert)))
    rows.extend(_price_rows("alert:price_set", alert.id, SSD_PRICE_KEYS, selected_price_key(alert)))
    rows.append(_alert_nav(alert.id))
    return InlineKeyboardMarkup(inline_keyboard=rows)


def build_alert_delete_keyboard(alert: Alert) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [InlineKeyboardButton(text=f"Confirmer suppression #{alert.id}", callback_data=f"alert:delete_confirm:{alert.id}")],
            _alert_nav(alert.id),
        ]
    )


def build_draft_keyboard(draft: AlertDraft) -> InlineKeyboardMarkup:
    nav = draft_nav_row()
    if draft.step == "media":
        rows = [[
            InlineKeyboardButton(text=_toggle_label("HDD", "rotational", draft.media_types), callback_data="draft:toggle:media:rotational"),
            InlineKeyboardButton(text=_toggle_label("SSD", "solid_state", draft.media_types), callback_data="draft:toggle:media:solid_state"),
        ], nav]
    elif draft.step == "condition":
        rows = [[
            InlineKeyboardButton(text=_toggle_label("New", "new", draft.conditions), callback_data="draft:toggle:condition:new"),
            InlineKeyboardButton(text=_toggle_label("Used", "used", draft.conditions), callback_data="draft:toggle:condition:used"),
        ], nav]
    elif draft.step == "capacity":
        selected = selected_capacity_keys(draft)
        all_label = _toggle_label(CAPACITY_PRESETS["all"][0], "all", ["all"] if not selected and draft.min_capacity_tb is None and draft.max_capacity_tb is None else [])
        rows = [[InlineKeyboardButton(text=all_label, callback_data="draft:cap:all")]]
        rows.extend(_preset_rows("draft:cap", None, HDD_CAPACITY_KEYS, selected))
        rows.extend(_preset_rows("draft:cap", None, SSD_CAPACITY_KEYS, selected))
        rows.append(nav)
    elif draft.step == "price":
        rows = [[InlineKeyboardButton(text=PRICE_PRESETS["none"][0], callback_data="draft:price:none")]]
        rows.extend(_price_rows("draft:price", None, HDD_PRICE_KEYS, selected_price_key(draft)))
        rows.extend(_price_rows("draft:price", None, SSD_PRICE_KEYS, selected_price_key(draft)))
        rows.append(nav)
    elif draft.step == "categories":
        rows = build_option_rows([*HDD_CATEGORIES, *SSD_CATEGORIES], draft.drive_categories, "draft:toggle:category")
        rows.append(nav)
    elif draft.step == "interfaces":
        rows = build_option_rows(INTERFACE_OPTIONS, draft.interfaces, "draft:toggle:interface")
        rows.append(nav)
    else:
        rows = [
            [InlineKeyboardButton(text="Creer", callback_data="draft:create")],
            [InlineKeyboardButton(text="Precedent", callback_data="draft:prev"), InlineKeyboardButton(text="Annuler", callback_data="draft:cancel")],
            [InlineKeyboardButton(text="Accueil", callback_data="menu:home")],
        ]
    return InlineKeyboardMarkup(inline_keyboard=rows)


def build_option_keyboard(options, selected: list[str], callback_prefix: str, nav_row: list[InlineKeyboardButton]) -> InlineKeyboardMarkup:
    rows = build_option_rows(options, selected, callback_prefix)
    rows.append(nav_row)
    return InlineKeyboardMarkup(inline_keyboard=rows)


def build_option_rows(options, selected: list[str], callback_prefix: str) -> list[list[InlineKeyboardButton]]:
    rows: list[list[InlineKeyboardButton]] = []
    current: list[InlineKeyboardButton] = []
    for label, value in options:
        current.append(InlineKeyboardButton(text=_toggle_label(label, value, selected), callback_data=f"{callback_prefix}:{value}"))
        if len(current) == 2:
            rows.append(current)
            current = []
    if current:
        rows.append(current)
    return rows


def _preset_rows(prefix: str, alert_id: int | None, keys: tuple[str, ...], selected: list[str]) -> list[list[InlineKeyboardButton]]:
    rows: list[list[InlineKeyboardButton]] = []
    current: list[InlineKeyboardButton] = []
    for key in keys:
        label = CAPACITY_PRESETS[key][0]
        text = f"[x] {label}" if key in selected else f"[ ] {label}"
        callback_data = f"{prefix}:{key}" if alert_id is None else f"{prefix}:{alert_id}:{key}"
        current.append(InlineKeyboardButton(text=text, callback_data=callback_data))
        if len(current) == 2:
            rows.append(current)
            current = []
    if current:
        rows.append(current)
    return rows


def _price_rows(prefix: str, alert_id: int | None, keys: tuple[str, ...], selected: str | None) -> list[list[InlineKeyboardButton]]:
    rows: list[list[InlineKeyboardButton]] = []
    current: list[InlineKeyboardButton] = []
    for key in keys:
        label = PRICE_PRESETS[key][0]
        text = f"[x] {label}" if key == selected else label
        callback_data = f"{prefix}:{key}" if alert_id is None else f"{prefix}:{alert_id}:{key}"
        current.append(InlineKeyboardButton(text=text, callback_data=callback_data))
        if len(current) == 2:
            rows.append(current)
            current = []
    if current:
        rows.append(current)
    return rows


def _alert_nav(alert_id: int) -> list[InlineKeyboardButton]:
    return [InlineKeyboardButton(text="Precedent", callback_data=f"alert:edit:{alert_id}"), InlineKeyboardButton(text="Accueil", callback_data="menu:home")]


def draft_nav_row() -> list[InlineKeyboardButton]:
    return [
        InlineKeyboardButton(text="Precedent", callback_data="draft:prev"),
        InlineKeyboardButton(text="Suivant", callback_data="draft:next"),
        InlineKeyboardButton(text="Accueil", callback_data="menu:home"),
    ]


def build_admin_users_keyboard(users, action: str | None = None) -> InlineKeyboardMarkup:
    rows: list[list[InlineKeyboardButton]] = []
    for user in users:
        if action == "revoke" and user.enabled:
            rows.append([InlineKeyboardButton(text=f"Revoquer {user.label}", callback_data=f"admin:revoke_user:{user.telegram_user_id}")])
        elif action == "reactivate" and not user.enabled:
            rows.append([InlineKeyboardButton(text=f"Reactiver {user.label}", callback_data=f"admin:reactivate_user:{user.telegram_user_id}")])
        elif action is None:
            rows.append([InlineKeyboardButton(text=format_authorized_user(user), callback_data="admin:list")])
    rows.append([InlineKeyboardButton(text="Precedent", callback_data="menu:admin"), InlineKeyboardButton(text="Accueil", callback_data="menu:home")])
    return InlineKeyboardMarkup(inline_keyboard=rows)


def _toggle_label(label: str, value: str, selected: list[str]) -> str:
    prefix = "[x]" if value in selected else "[ ]"
    return f"{prefix} {label}"


def _menu_parent(view: str) -> str:
    if view == "home":
        return "menu:home"
    if ":" not in view:
        return "menu:home"
    return f"menu:{view.rsplit(':', 1)[0]}"


def selected_capacity_keys(target: Alert | AlertDraft) -> list[str]:
    preset_keys = list(target.capacity_presets)
    if preset_keys:
        return preset_keys
    for key, (_, min_tb, max_tb, _) in CAPACITY_PRESETS.items():
        if target.min_capacity_tb == min_tb and target.max_capacity_tb == max_tb:
            return [key]
    return []


def selected_price_key(target: Alert | AlertDraft) -> str | None:
    if target.max_price_per_tb is None:
        return "none"
    for key, (_, price, _) in PRICE_PRESETS.items():
        if price is not None and Decimal(target.max_price_per_tb) == price:
            return key
    return None


def apply_capacity_preset_to_draft(draft: AlertDraft, key: str) -> None:
    _, min_tb, max_tb, media = CAPACITY_PRESETS[key]
    if key == "all":
        draft.capacity_presets = []
        draft.min_capacity_tb = min_tb
        draft.max_capacity_tb = max_tb
    elif key in draft.capacity_presets:
        draft.capacity_presets.remove(key)
    else:
        draft.capacity_presets.append(key)
        draft.min_capacity_tb = None
        draft.max_capacity_tb = None
    if key != "all" and media in VALID_MEDIA_TYPES and media not in draft.media_types:
        draft.media_types.append(media)
    draft.touch()


def apply_price_preset_to_draft(draft: AlertDraft, key: str) -> None:
    _, price, media = PRICE_PRESETS[key]
    draft.max_price_per_tb = price
    if media in VALID_MEDIA_TYPES and media not in draft.media_types:
        draft.media_types.append(media)
    draft.touch()


def draft_to_alert_args(draft: AlertDraft) -> AlertArgs:
    return AlertArgs(
        name=draft.name[:120] or "Alerte DiskCount",
        min_capacity_tb=draft.min_capacity_tb,
        max_capacity_tb=draft.max_capacity_tb,
        capacity_presets=list(draft.capacity_presets),
        conditions=draft.conditions or ["new", "used"],
        media_types=draft.media_types or ["rotational"],
        drive_categories=draft.drive_categories,
        interfaces=draft.interfaces,
        sources=draft.sources,
        max_price_per_tb=draft.max_price_per_tb,
        min_discount_pct=5.0,
        cooldown_hours=24,
    )


def create_alert_from_draft(repository: Repository, chat_id: int, owner_user_id: int, draft: AlertDraft) -> Alert:
    args = draft_to_alert_args(draft)
    return repository.create_alert(
        chat_id=chat_id,
        owner_user_id=owner_user_id,
        name=args.name,
        min_capacity_tb=args.min_capacity_tb,
        max_capacity_tb=args.max_capacity_tb,
        capacity_presets=args.capacity_presets,
        conditions=args.conditions,
        media_types=args.media_types,
        drive_categories=args.drive_categories,
        interfaces=args.interfaces,
        sources=args.sources,
        max_price_per_tb=args.max_price_per_tb,
        min_discount_pct=args.min_discount_pct,
        cooldown_hours=args.cooldown_hours,
    )


def next_step(step: str) -> str:
    index = WIZARD_STEPS.index(step)
    return WIZARD_STEPS[min(index + 1, len(WIZARD_STEPS) - 1)]


def previous_step(step: str) -> str:
    index = WIZARD_STEPS.index(step)
    return WIZARD_STEPS[max(index - 1, 0)]


def toggle_draft_value(draft: AlertDraft, field_name: str, value: str) -> None:
    mapping = {
        "condition": draft.conditions,
        "media": draft.media_types,
        "category": draft.drive_categories,
        "interface": draft.interfaces,
        "source": draft.sources,
    }
    values = mapping.get(field_name)
    if values is None:
        return
    if value in values:
        values.remove(value)
    else:
        values.append(value)
    draft.touch()


def menu_home_text(include_admin: bool = False) -> str:
    admin_line = "\nAdmin: ajoute, revoque ou reactive les utilisateurs." if include_admin else ""
    return (
        "DiskCount\n\n"
        "Choisis une action.\n\n"
        "Creer une alerte lance le wizard complet. Mes alertes ouvre tes alertes pour les modifier, "
        "les pauser ou les supprimer. Scanner/Test verifie le bot sans envoyer de notification."
        f"{admin_line}"
    )


def menu_static_text(view: str) -> str:
    texts = {
        "alerts": (
            "Alertes\n\n"
            "Cree une alerte ou ouvre tes alertes existantes. Chaque utilisateur autorise gere uniquement ses alertes."
        ),
        "alerts:add": (
            "Creer une alerte\n\n"
            "Utilise le bouton Creer une alerte pour le wizard par tuiles.\n\n"
            "Fallback avance:\n"
            "/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
            "category=internal_3_5,external_3_5 interface=sata,usb "
            "discount=5 cooldown=24\n\n"
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
            "DiskPrices, PricePerGig et PricePerTB couvrent les prix Amazon FR. Dealabs, Idealo, leDenicheur et "
            "leboncoin passent par des flux ou pages configurees. eBay utilise l'API officielle. Keepa est optionnel."
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
        "sources:pricepergig": (
            "PricePerGig\n\n"
            "Utilise l'API JSON publique PricePerGig filtree sur amazon.fr pour recuperer les HDD/SSD et leurs prix."
        ),
        "sources:pricepertb": (
            "PricePerTB\n\n"
            "Lit le tableau public PricePerTB FR pour recuperer les prix Amazon FR par To."
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
            "Consomme les flux/alertes IDEALO_FEED_URLS et peut rendre les pages IDEALO_PAGE_URLS en headless si "
            "le HTML direct ne donne pas d'offres."
        ),
        "sources:ledenicheur": (
            "leDenicheur\n\n"
            "Consomme les flux/alertes LEDENICHEUR_FEED_URLS et peut rendre les pages LEDENICHEUR_PAGE_URLS en "
            "headless si le HTML direct ne donne pas d'offres."
        ),
        "sources:keepa": (
            "Keepa\n\n"
            "Connecteur API optionnel. Il ne s'active que si KEEPA_API_KEY et KEEPA_ASINS sont definis."
        ),
        "help": (
            "Aide\n\n"
            "Choisis une fonction pour voir comment l'utiliser. Les tuiles executent les actions principales; "
            "les commandes texte restent disponibles comme raccourcis avances."
        ),
        "help:create": (
            "Guide - Creer une alerte\n\n"
            "Bouton: Creer une alerte.\n\n"
            "Le wizard te fait choisir dans l'ordre: type de disque, etat, capacites, prix, categories, connexions, "
            "puis recapitulatif.\n\n"
            "Tu peux cocher plusieurs capacites. Toute capacite vide la selection. Les sources ne sont pas demandees ici: "
            "elles sont configurees cote backend."
        ),
        "help:alerts": (
            "Guide - Mes alertes\n\n"
            "Bouton: Mes alertes.\n\n"
            "Chaque alerte apparait comme une tuile avec son nom, son etat, son type, sa capacite et son prix. "
            "Ouvre une tuile pour modifier l'alerte, la pauser, la reprendre ou la supprimer avec confirmation.\n\n"
            "Chaque utilisateur autorise voit uniquement ses propres alertes."
        ),
        "help:capacity": (
            "Guide - Capacites\n\n"
            "Les plages sont multi-selection.\n\n"
            "SSD: <256 Go, ~256 Go, ~512 Go, ~1 To, ~2 To, ~4 To, >4 To.\n"
            "HDD: <4 To, 4-8 To, 8-12 To, 12-16 To, 16-20 To, 20-24 To, 24-30 To, >30 To.\n\n"
            "Toute capacite retire le filtre. Les anciennes alertes texte avec min/max restent compatibles."
        ),
        "help:price": (
            "Guide - Prix\n\n"
            "HDD: seuils en EUR/To: 15, 18, 20, 22, 25 ou aucune limite.\n"
            "SSD: seuils en EUR/Go: 0.04, 0.06, 0.08, 0.10, 0.12 ou aucune limite.\n\n"
            "Le bot stocke toujours le prix en EUR/To. Pour SSD, le prix EUR/Go est converti automatiquement."
        ),
        "help:categories": (
            "Guide - Categories DiskPrices\n\n"
            "HDD: External 3.5, External 2.5, Internal 3.5, Internal 2.5, Internal Hybrid, Internal SAS.\n"
            "SSD: External SSD, Internal SSD, M.2 SATA, M.2 NVMe, U.2/U.3.\n\n"
            "Laisse vide pour accepter toutes les categories detectees."
        ),
        "help:interfaces": (
            "Guide - Connexions\n\n"
            "Choix disponibles: SATA, SAS, NVMe, USB.\n\n"
            "Laisse vide pour accepter toutes les connexions. Le bot deduit ces infos depuis les champs source "
            "et les titres quand elles sont disponibles."
        ),
        "help:scan": (
            "Guide - Scanner/Test\n\n"
            "Statut affiche les compteurs, les sources chargees et l'intervalle de scan.\n"
            "Test lance un dry-run limite a tes alertes: collecte, matching et calcul sans envoyer de notification "
            "et sans persister de nouvelle observation."
        ),
        "help:admin": (
            "Guide - Admin\n\n"
            "Visible uniquement pour les IDs dans TELEGRAM_ADMIN_USER_IDS.\n\n"
            "Utilisateurs liste les comptes connus. Ajouter demande un message au format: 123456789 Nom custom. "
            "Revoquer et Reactiver affichent les utilisateurs en boutons quand possible."
        ),
        "help:sources": (
            "Guide - Sources backend\n\n"
            "Les sources ne sont pas un filtre utilisateur dans Telegram. Elles sont gerees par la configuration du service: "
            "DiskPrices, PricePerGig API, PricePerTB, Dealabs RSS, eBay API, flux/pages Idealo/leDenicheur/leboncoin "
            "et Keepa API.\n\n"
            "Utilise /status pour voir les sources chargees."
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
            "discount: remise minimale face au prix habituel 30 jours.\n"
            "cooldown: delai en heures avant une re-notification."
        ),
        "help:commands": (
            "Commandes\n\n"
            "/menu ouvre ces tuiles.\n"
            "/create lance le wizard d'alerte.\n"
            "/alerts liste tes alertes.\n"
            "/add cree une alerte par texte, en fallback avance.\n"
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
        capacity_presets=[],
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
            if callback.message is not None:
                await callback.message.edit_text(format_alert_delete(alert), reply_markup=build_alert_delete_keyboard(alert))
            await callback.answer()
            return
        if action == "delete_confirm":
            repository.delete_alert(owner_user_id, alert_id)
            alerts = repository.list_alerts(owner_user_id=owner_user_id)
            if callback.message is not None:
                await callback.message.edit_text(format_alerts_list(alerts), reply_markup=build_alerts_keyboard(alerts, include_admin=include_admin))
            await callback.answer("Alerte supprimee.")
            return
        if action == "enabled":
            repository.set_alert_enabled(owner_user_id, alert_id, not alert.enabled)
            alert = repository.get_alert(owner_user_id, alert_id)

        if action == "toggle" and len(parts) == 5:
            field_name = parts[3]
            value = parts[4]
            if field_name not in {"condition", "media", "category", "interface", "source"}:
                await callback.answer("Filtre invalide.", show_alert=True)
                return
            alert = repository.toggle_alert_filter_value(owner_user_id, alert_id, field_name, value)

        if action == "cap" and len(parts) == 4:
            key = parts[3]
            if key in CAPACITY_PRESETS:
                _, _, _, media = CAPACITY_PRESETS[key]
                alert = repository.toggle_alert_capacity_preset(owner_user_id, alert_id, key)
                if alert is not None and key in alert.capacity_presets and media in VALID_MEDIA_TYPES:
                    current = repository.get_alert(owner_user_id, alert_id)
                    if current is not None and media not in current.media_types:
                        repository.toggle_alert_filter_value(owner_user_id, alert_id, "media", media)
                alert = repository.get_alert(owner_user_id, alert_id)

        if action == "price_set" and len(parts) == 4:
            key = parts[3]
            if key in PRICE_PRESETS:
                _, price, media = PRICE_PRESETS[key]
                repository.set_alert_max_price_per_tb(owner_user_id, alert_id, price)
                if media in VALID_MEDIA_TYPES:
                    current = repository.get_alert(owner_user_id, alert_id)
                    if current is not None and media not in current.media_types:
                        repository.toggle_alert_filter_value(owner_user_id, alert_id, "media", media)
                alert = repository.get_alert(owner_user_id, alert_id)

        if alert is None:
            await callback.answer("Alerte introuvable.", show_alert=True)
            return

        if action == "media":
            await edit_alert_message(callback, format_alert_media(alert), build_alert_media_keyboard(alert))
        elif action == "condition":
            await edit_alert_message(callback, format_alert_condition(alert), build_alert_condition_keyboard(alert))
        elif action in {"capacity", "cap"}:
            await edit_alert_message(callback, format_alert_capacity(alert), build_alert_capacity_keyboard(alert))
        elif action in {"price", "price_set"}:
            await edit_alert_message(callback, format_alert_price(alert), build_alert_price_keyboard(alert))
        elif action == "categories" or (action == "toggle" and len(parts) == 5 and parts[3] == "category"):
            await edit_alert_message(callback, format_alert_categories(alert), build_alert_category_keyboard(alert))
        elif action == "interfaces" or (action == "toggle" and len(parts) == 5 and parts[3] == "interface"):
            await edit_alert_message(callback, format_alert_interfaces(alert), build_alert_interface_keyboard(alert))
        else:
            await edit_alert_message(callback, format_alert_detail(alert), build_alert_edit_keyboard(alert, include_admin=include_admin))
        await callback.answer()

    async def edit_alert_message(callback: CallbackQuery, text: str, keyboard: InlineKeyboardMarkup) -> None:
        if callback.message is not None:
            await callback.message.edit_text(text, reply_markup=keyboard)
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
            capacity_presets=[],
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

    alert_drafts: dict[int, AlertDraft] = {}
    admin_pending: dict[int, str] = {}

    def get_draft(user_id: int) -> AlertDraft:
        draft = alert_drafts.get(user_id)
        if draft is None or (time.time() - draft.updated_at) > DRAFT_TTL_SECONDS:
            draft = AlertDraft()
            alert_drafts[user_id] = draft
        draft.touch()
        return draft

    async def show_draft(callback: CallbackQuery, draft: AlertDraft) -> None:
        if callback.message is not None:
            await callback.message.edit_text(format_draft(draft), reply_markup=build_draft_keyboard(draft))
        await callback.answer()

    @router.message(Command("create"))
    async def create_alert_command(message: Message) -> None:
        if not await guard(message):
            return
        user_id = current_user_id(message)
        alert_drafts[user_id] = AlertDraft()
        await message.answer(format_draft(alert_drafts[user_id]), reply_markup=build_draft_keyboard(alert_drafts[user_id]))

    @router.callback_query(lambda callback: bool(callback.data and callback.data.startswith("draft:")))
    async def draft_callback(callback: CallbackQuery) -> None:
        if not is_authorized(settings, repository, callback.from_user.id if callback.from_user else None):
            await callback.answer("Acces refuse.", show_alert=True)
            return
        user_id = callback.from_user.id
        data = callback.data or ""
        if data == "draft:start":
            alert_drafts[user_id] = AlertDraft()
            await show_draft(callback, alert_drafts[user_id])
            return
        if data == "draft:cancel":
            alert_drafts.pop(user_id, None)
            await edit_menu(callback, "home", "Creation annulee.\n\nChoisis une action.")
            return
        draft = get_draft(user_id)
        if data == "draft:next":
            draft.step = next_step(draft.step)
            draft.touch()
            await show_draft(callback, draft)
            return
        if data == "draft:prev":
            draft.step = previous_step(draft.step)
            draft.touch()
            await show_draft(callback, draft)
            return
        if data.startswith("draft:toggle:"):
            _, _, field_name, value = data.split(":", 3)
            toggle_draft_value(draft, field_name, value)
            await show_draft(callback, draft)
            return
        if data.startswith("draft:cap:"):
            key = data.rsplit(":", 1)[1]
            if key in CAPACITY_PRESETS:
                apply_capacity_preset_to_draft(draft, key)
            await show_draft(callback, draft)
            return
        if data.startswith("draft:price:"):
            key = data.rsplit(":", 1)[1]
            if key in PRICE_PRESETS:
                apply_price_preset_to_draft(draft, key)
            await show_draft(callback, draft)
            return
        if data == "draft:create":
            alert = create_alert_from_draft(repository, callback.message.chat.id, user_id, draft)
            alert_drafts.pop(user_id, None)
            if callback.message is not None:
                await callback.message.edit_text(format_alert_detail(alert), reply_markup=build_alert_edit_keyboard(alert, include_admin=include_admin_for_callback(callback)))
            await callback.answer("Alerte creee.")
            return
        await callback.answer()

    @router.callback_query(lambda callback: bool(callback.data and callback.data.startswith("admin:")))
    async def admin_callback(callback: CallbackQuery) -> None:
        if not is_env_admin(settings, callback.from_user.id if callback.from_user else None):
            await callback.answer("Commande reservee a l'administrateur.", show_alert=True)
            return
        data = callback.data or ""
        if data == "admin:list":
            users = repository.list_authorized_users(include_disabled=True)
            if callback.message is not None:
                await callback.message.edit_text(format_authorized_users_list(users), reply_markup=build_admin_users_keyboard(users))
            await callback.answer()
            return
        if data == "admin:add":
            admin_pending[callback.from_user.id] = "allow"
            if callback.message is not None:
                await callback.message.edit_text("Ajouter un utilisateur\n\nEnvoie maintenant: 123456789 Nom custom", reply_markup=build_menu_keyboard("admin", include_admin=True))
            await callback.answer()
            return
        if data == "admin:revoke":
            users = repository.list_authorized_users(include_disabled=True)
            if callback.message is not None:
                await callback.message.edit_text("Revoquer un utilisateur", reply_markup=build_admin_users_keyboard(users, action="revoke"))
            await callback.answer()
            return
        if data == "admin:reactivate":
            users = repository.list_authorized_users(include_disabled=True)
            if callback.message is not None:
                await callback.message.edit_text("Reactiver un utilisateur", reply_markup=build_admin_users_keyboard(users, action="reactivate"))
            await callback.answer()
            return
        if data.startswith("admin:revoke_user:"):
            user_id = _int(data.rsplit(":", 1)[1])
            if user_id is not None:
                repository.revoke_authorized_user(user_id)
            users = repository.list_authorized_users(include_disabled=True)
            if callback.message is not None:
                await callback.message.edit_text(format_authorized_users_list(users), reply_markup=build_admin_users_keyboard(users))
            await callback.answer("Utilisateur revoque.")
            return
        if data.startswith("admin:reactivate_user:"):
            user_id = _int(data.rsplit(":", 1)[1])
            if user_id is not None:
                user = next((item for item in repository.list_authorized_users(include_disabled=True) if item.telegram_user_id == user_id), None)
                if user is not None:
                    repository.upsert_authorized_user(user.telegram_user_id, user.label, is_admin=user.is_admin)
            users = repository.list_authorized_users(include_disabled=True)
            if callback.message is not None:
                await callback.message.edit_text(format_authorized_users_list(users), reply_markup=build_admin_users_keyboard(users))
            await callback.answer("Utilisateur reactive.")
            return
        await callback.answer()

    @router.message(lambda message: bool(message.from_user and admin_pending.get(message.from_user.id)))
    async def admin_pending_message(message: Message) -> None:
        if not await admin_guard(message):
            return
        if message.text and message.text.startswith("/"):
            admin_pending.pop(message.from_user.id, None)
            await message.answer("Action admin annulee.", reply_markup=build_menu_keyboard("admin", include_admin=True))
            return
        action = admin_pending.get(message.from_user.id)
        if action != "allow":
            return
        parsed = _user_id_and_label(message.text)
        if parsed is None:
            await message.answer("Format attendu: 123456789 Nom custom", reply_markup=build_menu_keyboard("admin", include_admin=True))
            return
        user_id, label = parsed
        admin_pending.pop(message.from_user.id, None)
        user = repository.upsert_authorized_user(user_id, label)
        await message.answer(f"Utilisateur autorise: {format_authorized_user(user)}", reply_markup=build_menu_keyboard("admin", include_admin=True))

    dispatcher = Dispatcher()
    dispatcher.include_router(router)
    return dispatcher


def format_alert(alert: Alert) -> str:
    state = "on" if alert.enabled else "off"
    parts = [
        f"#{alert.id} [{state}] {alert.name}",
        f"capacite={format_capacity_filter(alert)}",
        format_price_limit(alert),
        f"remise>={alert.min_discount_pct:g}%",
        f"etat={','.join(alert.conditions)}" if alert.conditions else None,
        f"type={','.join(alert.media_types)}" if alert.media_types else None,
        f"cat={','.join(alert.drive_categories)}" if alert.drive_categories else None,
        f"conn={','.join(alert.interfaces)}" if alert.interfaces else None,
    ]
    return " | ".join(part for part in parts if part)


def format_price_limit(alert: Alert) -> str | None:
    if alert.max_price_per_tb is None:
        return None
    price = Decimal(alert.max_price_per_tb)
    if alert.media_types == ["solid_state"]:
        return f"prix<={price / Decimal('1000'):g}EUR/Go"
    return f"prix<={price:g}EUR/To"


def format_alert_button(alert: Alert) -> str:
    state = "on" if alert.enabled else "off"
    media = ",".join(_display_value(value) for value in alert.media_types) or "HDD/SSD"
    capacity = format_capacity_filter(alert)
    price = format_price_limit(alert) or "prix libre"
    return f"#{alert.id} {alert.name} | {state} | {media} | {capacity} | {price}"


def format_capacity_range(min_capacity_tb: float | None, max_capacity_tb: float | None) -> str:
    if min_capacity_tb is None and max_capacity_tb is None:
        return "toute capacite"
    if min_capacity_tb is None:
        return f"<={max_capacity_tb:g} To"
    if max_capacity_tb is None:
        return f">={min_capacity_tb:g} To"
    return f"{min_capacity_tb:g}-{max_capacity_tb:g} To"


def format_capacity_filter(alert: Alert) -> str:
    if alert.capacity_presets:
        return ", ".join(CAPACITY_PRESETS[key][0] for key in alert.capacity_presets if key in CAPACITY_PRESETS) or "toute capacite"
    return format_capacity_range(alert.min_capacity_tb, alert.max_capacity_tb)


def format_alert_media(alert: Alert) -> str:
    return (
        "Type de disque\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Coche HDD pour rotational, SSD pour solid state, ou les deux."
    )


def format_alert_condition(alert: Alert) -> str:
    return (
        "Etat produit\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Coche New, Used, ou les deux selon ce que tu acceptes."
    )


def format_alert_capacity(alert: Alert) -> str:
    return (
        "Plage de stockage\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Choisis une plage. Les presets SSD sont en Go/To, les presets HDD en To."
    )


def format_alert_price(alert: Alert) -> str:
    return (
        "Prix maximum\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Choisis un seuil. Les boutons SSD affichent EUR/Go et sont stockes en EUR/To en interne."
    )


def format_alert_categories(alert: Alert) -> str:
    return (
        "Categories DiskPrices\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Coche les familles voulues: externe, interne, 2.5, 3.5, SAS, SSD, M.2 ou U.2/U.3."
    )


def format_alert_interfaces(alert: Alert) -> str:
    return (
        "Connexions\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Coche SATA, SAS, NVMe ou USB selon les offres que tu veux recevoir."
    )


def format_alert_delete(alert: Alert) -> str:
    return (
        "Supprimer l'alerte\n\n"
        f"{format_alert_button(alert)}\n\n"
        "Confirme seulement si tu veux supprimer cette alerte."
    )


def format_draft(draft: AlertDraft) -> str:
    titles = {
        "media": "1/8 Type de disque",
        "condition": "2/8 Etat produit",
        "capacity": "3/8 Capacite",
        "price": "4/8 Prix",
        "categories": "5/8 Categories DiskPrices",
        "interfaces": "6/8 Connexions",
        "confirm": "7/7 Recapitulatif",
    }
    hints = {
        "media": "Choisis HDD, SSD, ou les deux.",
        "condition": "Choisis New, Used, ou les deux.",
        "capacity": "Choisis une plage de stockage predefinie.",
        "price": "Choisis un prix maximum par To ou par Go selon le type.",
        "categories": "Filtre les familles DiskPrices: interne, externe, M.2, SAS, etc.",
        "interfaces": "Filtre les connexions: SATA, SAS, NVMe ou USB.",
        "confirm": "Verifie le recapitulatif puis cree l'alerte.",
    }
    return f"{titles.get(draft.step, 'Creation alerte')}\n\n{format_draft_summary(draft)}\n\n{hints.get(draft.step, '')}"


def format_draft_summary(draft: AlertDraft) -> str:
    return (
        f"Nom: {draft.name}\n"
        f"Type: {format_values(draft.media_types)}\n"
        f"Etat: {format_values(draft.conditions)}\n"
        f"Capacite: {format_draft_capacity(draft)}\n"
        f"Prix max: {format_draft_price(draft)}\n"
        f"Categories: {format_values(draft.drive_categories)}\n"
        f"Connexions: {format_values(draft.interfaces)}"
    )


def format_draft_capacity(draft: AlertDraft) -> str:
    if draft.capacity_presets:
        return ", ".join(CAPACITY_PRESETS[key][0] for key in draft.capacity_presets if key in CAPACITY_PRESETS) or "toute capacite"
    return format_capacity_range(draft.min_capacity_tb, draft.max_capacity_tb)


def format_draft_price(draft: AlertDraft) -> str:
    if draft.max_price_per_tb is None:
        return "aucune limite"
    price = Decimal(draft.max_price_per_tb)
    if draft.media_types == ["solid_state"]:
        return f"{price / Decimal('1000'):g} EUR/Go"
    return f"{price:g} EUR/To"


def format_values(values: list[str]) -> str:
    if not values:
        return "tous"
    return ", ".join(_display_value(value) for value in values)


def _display_value(value: str) -> str:
    labels = {
        "rotational": "HDD",
        "solid_state": "SSD",
        "new": "New",
        "used": "Used",
        "external_3_5": "External 3.5",
        "external_2_5": "External 2.5",
        "internal_3_5": "Internal 3.5",
        "internal_2_5": "Internal 2.5",
        "internal_hybrid": "Internal Hybrid",
        "internal_sas": "Internal SAS",
        "external_ssd": "External SSD",
        "internal_ssd": "Internal SSD",
        "m2_sata": "M.2 SATA",
        "m2_nvme": "M.2 NVMe",
        "u2_u3": "U.2/U.3",
        "sata": "SATA",
        "sas": "SAS",
        "nvme": "NVMe",
        "usb": "USB",
        "diskprices": "DiskPrices",
        "pricepergig": "PricePerGig",
        "pricepertb": "PricePerTB",
        "dealabs": "Dealabs",
        "ebay": "eBay",
        "leboncoin": "leboncoin",
        "idealo": "Idealo",
        "ledenicheur": "leDenicheur",
        "keepa": "Keepa",
    }
    return labels.get(value, value)


def format_alerts_list(alerts: list[Alert]) -> str:
    if not alerts:
        return (
            "Mes alertes\n\n"
            "Aucune alerte pour ton compte.\n\n"
            "Utilise Creer une alerte pour demarrer avec les tuiles."
        )
    return "Mes alertes\n\n" + "\n".join(format_alert(alert) for alert in alerts)


def format_alert_detail(alert: Alert) -> str:
    return (
        "Modifier une notification\n\n"
        f"{format_alert(alert)}\n\n"
        "Ouvre une categorie pour modifier directement ses valeurs."
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
