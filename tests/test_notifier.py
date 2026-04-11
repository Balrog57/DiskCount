from decimal import Decimal

from diskcount.db import Alert
from diskcount.domain import Deal, NotificationDecision
from diskcount.notifier import deal_keyboard, format_deal_message


def test_format_deal_message_and_keyboard() -> None:
    alert = Alert(id=1, chat_id=42, name="NAS", min_discount_pct=5.0, cooldown_hours=24)
    deal = Deal(
        source="ebay",
        title="Seagate Exos 16 To HDD",
        url="https://www.ebay.fr/itm/123",
        price_eur=Decimal("280.00"),
        price_per_tb=Decimal("17.50"),
        capacity_tb=Decimal("16"),
        condition="used",
        media_type="rotational",
    )
    decision = NotificationDecision(True, "max_price_per_tb", Decimal("10.00"), Decimal("19.50"))

    assert "17.50 EUR/To" in format_deal_message(alert, deal, decision)
    keyboard = deal_keyboard(deal)
    assert keyboard is not None
    assert keyboard.inline_keyboard[0][0].url == deal.url
