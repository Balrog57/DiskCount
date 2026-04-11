from __future__ import annotations

from datetime import datetime, timedelta, timezone
from decimal import Decimal

from .db import Alert, Notification
from .domain import Deal, NotificationDecision


def alert_matches(alert: Alert, deal: Deal) -> bool:
    if alert.sources and deal.source not in alert.sources:
        return False
    if alert.conditions and deal.condition not in alert.conditions:
        return False
    if alert.media_types and deal.media_type not in alert.media_types:
        return False
    if alert.min_capacity_tb is not None and deal.capacity_tb < Decimal(str(alert.min_capacity_tb)):
        return False
    if alert.max_capacity_tb is not None and deal.capacity_tb > Decimal(str(alert.max_capacity_tb)):
        return False
    if alert.max_price_per_tb is not None and deal.price_per_tb > Decimal(alert.max_price_per_tb):
        return False
    return True


def should_notify(
    alert: Alert,
    deal: Deal,
    baseline_price_per_tb: Decimal | None,
    last_notification: Notification | None,
    now: datetime,
    significant_drop_pct: float,
) -> NotificationDecision:
    discount_pct: Decimal | None = None
    discount_hit = False
    threshold_hit = alert.max_price_per_tb is not None and deal.price_per_tb <= Decimal(alert.max_price_per_tb)

    if baseline_price_per_tb is not None and baseline_price_per_tb > 0:
        min_factor = Decimal("1") - (Decimal(str(alert.min_discount_pct)) / Decimal("100"))
        discount_hit = deal.price_per_tb <= (baseline_price_per_tb * min_factor)
        discount_pct = ((baseline_price_per_tb - deal.price_per_tb) / baseline_price_per_tb * Decimal("100")).quantize(
            Decimal("0.01")
        )

    if not threshold_hit and not discount_hit:
        return NotificationDecision(False, "no_threshold", discount_pct, baseline_price_per_tb)

    if last_notification is not None:
        cooldown_until = _as_aware(last_notification.sent_at) + timedelta(hours=alert.cooldown_hours)
        drop_factor = Decimal("1") - (Decimal(str(significant_drop_pct)) / Decimal("100"))
        dropped_further = deal.price_per_tb <= (Decimal(last_notification.price_per_tb) * drop_factor)
        if now < cooldown_until and not dropped_further:
            return NotificationDecision(False, "cooldown", discount_pct, baseline_price_per_tb)

    reason = "max_price_per_tb" if threshold_hit else "rolling_discount"
    return NotificationDecision(True, reason, discount_pct, baseline_price_per_tb)


def _as_aware(value: datetime) -> datetime:
    if value.tzinfo is None:
        return value.replace(tzinfo=timezone.utc)
    return value
