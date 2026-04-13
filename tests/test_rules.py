from dataclasses import replace
from datetime import timedelta
from decimal import Decimal

from diskcount.db import Alert, Notification
from diskcount.domain import Deal, utc_now
from diskcount.rules import alert_matches, should_notify


def make_alert(**kwargs) -> Alert:
    defaults = {
        "id": 1,
        "chat_id": 42,
        "name": "NAS",
        "min_capacity_tb": 16,
        "max_capacity_tb": None,
        "conditions_json": '["new","used"]',
        "media_types_json": '["rotational"]',
        "sources_json": '["diskprices"]',
        "max_price_per_tb": Decimal("20.00"),
        "min_discount_pct": 5.0,
        "cooldown_hours": 24,
        "enabled": True,
    }
    defaults.update(kwargs)
    return Alert(**defaults)


def make_deal(price_per_tb: Decimal = Decimal("19.00")) -> Deal:
    return Deal(
        source="diskprices",
        title="WD 16 To",
        url="https://example.com/wd",
        price_eur=Decimal("304.00"),
        price_per_tb=price_per_tb,
        capacity_tb=Decimal("16"),
        condition="new",
        media_type="rotational",
        drive_category="internal_3_5",
        interfaces=("sata",),
    )


def test_threshold_match_without_history() -> None:
    alert = make_alert()
    deal = make_deal()
    assert alert_matches(alert, deal)
    decision = should_notify(alert, deal, None, None, utc_now(), significant_drop_pct=2)
    assert decision.should_notify
    assert decision.reason == "max_price_per_tb"


def test_alert_matches_diskprice_category_and_interface() -> None:
    alert = make_alert(drive_categories_json='["internal_3_5"]', interfaces_json='["sata"]')
    assert alert_matches(alert, make_deal())

    assert not alert_matches(alert, replace(make_deal(), drive_category="external_3_5"))
    assert not alert_matches(alert, replace(make_deal(), interfaces=("usb",)))


def test_alert_matches_multiple_capacity_presets() -> None:
    alert = make_alert(min_capacity_tb=None, max_capacity_tb=None, capacity_presets_json='["hdd_16_20","hdd_20_24"]')
    assert alert_matches(alert, replace(make_deal(), capacity_tb=Decimal("18")))
    assert alert_matches(alert, replace(make_deal(), capacity_tb=Decimal("22")))
    assert not alert_matches(alert, replace(make_deal(), capacity_tb=Decimal("28")))


def test_rolling_discount_match() -> None:
    alert = make_alert(max_price_per_tb=None)
    deal = make_deal(Decimal("90.00"))
    decision = should_notify(alert, deal, Decimal("100.00"), None, utc_now(), significant_drop_pct=2)
    assert decision.should_notify
    assert decision.reason == "rolling_discount"
    assert decision.discount_pct == Decimal("10.00")


def test_cooldown_blocks_same_price() -> None:
    alert = make_alert()
    deal = make_deal()
    now = utc_now()
    last = Notification(
        alert_id=1,
        product_id=deal.product_id,
        sent_at=now - timedelta(hours=1),
        price_eur=deal.price_eur,
        price_per_tb=deal.price_per_tb,
        discount_pct=None,
        reason="max_price_per_tb",
        title=deal.title,
        url=deal.url,
    )
    decision = should_notify(alert, deal, None, last, now, significant_drop_pct=2)
    assert not decision.should_notify
    assert decision.reason == "cooldown"
