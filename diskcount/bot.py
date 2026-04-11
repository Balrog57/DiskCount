from __future__ import annotations

import shlex
from dataclasses import dataclass
from decimal import Decimal

from aiogram import Bot
from aiogram import Dispatcher, Router
from aiogram.filters import Command, CommandObject, CommandStart
from aiogram.types import BotCommand, BotCommandScopeChat, BotCommandScopeDefault, KeyboardButton, Message, ReplyKeyboardMarkup

from .config import Settings
from .db import Alert, Repository
from .scanner import Scanner

VALID_CONDITIONS = {"new", "used"}
VALID_MEDIA_TYPES = {"rotational", "solid_state"}
VALID_SOURCES = {"diskprices", "dealabs", "idealo", "ledenicheur", "leboncoin", "ebay", "keepa"}

USER_COMMANDS: tuple[tuple[str, str], ...] = (
    ("start", "Demarrer le bot et enregistrer le chat"),
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
        [KeyboardButton(text="/alerts"), KeyboardButton(text="/add")],
        [KeyboardButton(text="/test"), KeyboardButton(text="/status")],
        [KeyboardButton(text="/pause"), KeyboardButton(text="/resume")],
        [KeyboardButton(text="/delete"), KeyboardButton(text="/set_max_price")],
        [KeyboardButton(text="/help")],
    ]
    if include_admin:
        rows.append([KeyboardButton(text="/users"), KeyboardButton(text="/allow"), KeyboardButton(text="/revoke")])
    return ReplyKeyboardMarkup(keyboard=rows, resize_keyboard=True, is_persistent=True)


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

    @router.message(CommandStart())
    async def start(message: Message) -> None:
        if not await guard(message):
            return
        repository.upsert_subscriber(message.chat.id, message.from_user.username if message.from_user else None)
        include_admin = is_env_admin(settings, message.from_user.id if message.from_user else None)
        await message.answer(
            "DiskCount est pret.\n"
            "Exemple: /add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used\n"
            "Tape / pour afficher le menu des commandes Telegram, ou utilise les tuiles ci-dessous.",
            reply_markup=build_main_keyboard(include_admin=include_admin),
        )

    @router.message(Command("help"))
    async def help_command(message: Message) -> None:
        if not await guard(message):
            return
        include_admin = is_env_admin(settings, message.from_user.id if message.from_user else None)
        await message.answer(
            "Exemple:\n"
            "/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
            "discount=5 sources=diskprices,dealabs,ebay,leboncoin\n\n"
            "Modifier le prix max: /set_max_price 1 18.5 ou /set_max_price 1 none\n\n"
            "Admin: /users, /allow 123456 Nom custom, /revoke 123456\n\n"
            "Filtres: min_tb, max_tb, max_eur_tb, condition=new|used, media=rotational|solid_state, "
            "sources=diskprices|dealabs|idealo|ledenicheur|leboncoin|ebay|keepa, discount, cooldown.",
            reply_markup=build_main_keyboard(include_admin=include_admin),
        )

    @router.message(Command("users"))
    async def users(message: Message) -> None:
        if not await admin_guard(message):
            return
        rows = repository.list_authorized_users(include_disabled=True)
        if not rows:
            await message.answer("Aucun utilisateur autorise en base.")
            return
        await message.answer("\n".join(format_authorized_user(user) for user in rows))

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
        await message.answer(f"Utilisateur autorise: {format_authorized_user(user)}")

    @router.message(Command("revoke"))
    async def revoke(message: Message, command: CommandObject) -> None:
        if not await admin_guard(message):
            return
        user_id = _user_id(command.args)
        if user_id is None:
            await message.answer("Usage: /revoke 123456789")
            return
        if not repository.revoke_authorized_user(user_id):
            await message.answer("Utilisateur introuvable.")
            return
        await message.answer(f"Utilisateur {user_id} desactive.")

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
        await message.answer(f"Alerte #{alert.id} creee: {format_alert(alert)}")

    @router.message(Command("alerts"))
    async def alerts(message: Message) -> None:
        if not await guard(message):
            return
        rows = repository.list_alerts(owner_user_id=current_user_id(message))
        if not rows:
            await message.answer("Aucune alerte.")
            return
        await message.answer("\n".join(format_alert(alert) for alert in rows))

    @router.message(Command("pause"))
    async def pause(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.set_alert_enabled(current_user_id(message), alert_id, False):
            await message.answer("Alerte introuvable. Usage: /pause 1")
            return
        await message.answer(f"Alerte #{alert_id} en pause.")

    @router.message(Command("resume"))
    async def resume(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.set_alert_enabled(current_user_id(message), alert_id, True):
            await message.answer("Alerte introuvable. Usage: /resume 1")
            return
        await message.answer(f"Alerte #{alert_id} activee.")

    @router.message(Command("delete"))
    async def delete(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.delete_alert(current_user_id(message), alert_id):
            await message.answer("Alerte introuvable. Usage: /delete 1")
            return
        await message.answer(f"Alerte #{alert_id} supprimee.")

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
        await message.answer(f"Prix max de l'alerte #{alert_id}: {value}.")

    @router.message(Command("test"))
    async def test_scan(message: Message) -> None:
        if not await guard(message):
            return
        report = await scanner.run_once(dry_run=True, target_owner_user_id=current_user_id(message))
        await message.answer(
            f"Dry-run termine: {report.fetched} offres, {report.matched} matchs, "
            f"{report.dry_run_notifications} notifications potentielles, {len(report.errors)} erreurs."
        )

    @router.message(Command("status"))
    async def status(message: Message) -> None:
        if not await guard(message):
            return
        counts = repository.counts()
        await message.answer(
            "DiskCount status\n"
            f"Sources: {', '.join(source.name for source in scanner.sources)}\n"
            f"Alertes: {counts['alerts']} | Produits: {counts['products']} | "
            f"Observations: {counts['observations']} | Notifications: {counts['notifications']} | "
            f"Utilisateurs: {counts['authorized_users']}\n"
            f"Intervalle: {settings.poll_interval_seconds}s"
        )

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


def format_authorized_user(user) -> str:
    state = "on" if user.enabled else "off"
    role = "admin" if user.is_admin else "user"
    return f"{user.label} | {user.telegram_user_id} | {role} | {state}"


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
