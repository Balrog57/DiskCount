from __future__ import annotations

from decimal import Decimal
from typing import Protocol

from aiogram import Bot
from aiogram.types import InlineKeyboardButton, InlineKeyboardMarkup

from .db import Alert
from .domain import Deal, NotificationDecision


class Notifier(Protocol):
    async def send_deal(self, chat_id: int, alert: Alert, deal: Deal, decision: NotificationDecision) -> None:
        raise NotImplementedError


class TelegramNotifier:
    def __init__(self, bot: Bot) -> None:
        self.bot = bot

    async def send_deal(self, chat_id: int, alert: Alert, deal: Deal, decision: NotificationDecision) -> None:
        await self.bot.send_message(
            chat_id=chat_id,
            text=format_deal_message(alert, deal, decision),
            disable_web_page_preview=True,
            reply_markup=deal_keyboard(deal),
        )


def money(value: Decimal | None, suffix: str = "EUR") -> str:
    if value is None:
        return "n/a"
    return f"{value:.2f} {suffix}"


def format_deal_message(alert: Alert, deal: Deal, decision: NotificationDecision) -> str:
    discount = f"{decision.discount_pct:.2f}%" if decision.discount_pct is not None else "n/a"
    baseline = money(decision.baseline_price_per_tb, "EUR/To")
    return "\n".join(
        [
            f"Bon plan DiskCount: {alert.name}",
            deal.title,
            f"Prix: {money(deal.price_eur)} ({money(deal.price_per_tb, 'EUR/To')})",
            f"Capacite: {deal.capacity_tb:g} To | Etat: {deal.condition or 'n/a'} | Type: {deal.media_type or 'n/a'}",
            f"Source: {deal.source} | Declencheur: {decision.reason} | Remise: {discount} vs {baseline}",
            deal.url,
        ]
    )


def deal_keyboard(deal: Deal) -> InlineKeyboardMarkup | None:
    if not deal.url:
        return None
    return InlineKeyboardMarkup(
        inline_keyboard=[[InlineKeyboardButton(text="Ouvrir l'offre", url=deal.url)]]
    )
