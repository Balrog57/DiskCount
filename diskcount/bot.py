from __future__ import annotations

import shlex
from dataclasses import dataclass
from decimal import Decimal

from aiogram import Dispatcher, Router
from aiogram.filters import Command, CommandObject, CommandStart
from aiogram.types import Message

from .config import Settings
from .db import Alert, Repository
from .scanner import Scanner

VALID_CONDITIONS = {"new", "used"}
VALID_MEDIA_TYPES = {"rotational", "solid_state"}
VALID_SOURCES = {"diskprices", "dealabs", "idealo", "ledenicheur", "leboncoin", "ebay", "keepa"}


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


def is_authorized(settings: Settings, user_id: int | None) -> bool:
    if user_id is None:
        return False
    return not settings.telegram_allowed_user_ids or user_id in settings.telegram_allowed_user_ids


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
        if is_authorized(settings, message.from_user.id if message.from_user else None):
            return True
        await message.answer("Acces refuse.")
        return False

    @router.message(CommandStart())
    async def start(message: Message) -> None:
        if not await guard(message):
            return
        repository.upsert_subscriber(message.chat.id, message.from_user.username if message.from_user else None)
        await message.answer(
            "DiskCount est pret.\n"
            "Exemple: /add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used\n"
            "Commandes: /alerts, /pause, /resume, /delete, /set_max_price, /test, /status"
        )

    @router.message(Command("help"))
    async def help_command(message: Message) -> None:
        if not await guard(message):
            return
        await message.answer(
            "Exemple:\n"
            "/add name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
            "discount=5 sources=diskprices,dealabs,ebay,leboncoin\n\n"
            "Modifier le prix max: /set_max_price 1 18.5 ou /set_max_price 1 none\n\n"
            "Filtres: min_tb, max_tb, max_eur_tb, condition=new|used, media=rotational|solid_state, "
            "sources=diskprices|dealabs|idealo|ledenicheur|leboncoin|ebay|keepa, discount, cooldown."
        )

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
        rows = repository.list_alerts(chat_id=message.chat.id)
        if not rows:
            await message.answer("Aucune alerte.")
            return
        await message.answer("\n".join(format_alert(alert) for alert in rows))

    @router.message(Command("pause"))
    async def pause(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.set_alert_enabled(message.chat.id, alert_id, False):
            await message.answer("Alerte introuvable. Usage: /pause 1")
            return
        await message.answer(f"Alerte #{alert_id} en pause.")

    @router.message(Command("resume"))
    async def resume(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.set_alert_enabled(message.chat.id, alert_id, True):
            await message.answer("Alerte introuvable. Usage: /resume 1")
            return
        await message.answer(f"Alerte #{alert_id} activee.")

    @router.message(Command("delete"))
    async def delete(message: Message, command: CommandObject) -> None:
        if not await guard(message):
            return
        alert_id = _alert_id(command.args)
        if alert_id is None or not repository.delete_alert(message.chat.id, alert_id):
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
        if not repository.set_alert_max_price_per_tb(message.chat.id, alert_id, price):
            await message.answer("Alerte introuvable.")
            return
        value = "desactive" if price is None else f"{price:g} EUR/To"
        await message.answer(f"Prix max de l'alerte #{alert_id}: {value}.")

    @router.message(Command("test"))
    async def test_scan(message: Message) -> None:
        if not await guard(message):
            return
        report = await scanner.run_once(dry_run=True, target_chat_id=message.chat.id)
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
            f"Observations: {counts['observations']} | Notifications: {counts['notifications']}\n"
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
