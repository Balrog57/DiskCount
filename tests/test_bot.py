from decimal import Decimal

from diskcount.bot import _alert_id_and_price, is_authorized, parse_alert_args
from diskcount.config import Settings


def test_parse_alert_args() -> None:
    args = parse_alert_args(
        "name=NAS min_tb=16 max_eur_tb=20 media=rotational condition=new,used "
        "sources=diskprices,ebay,leboncoin"
    )
    assert args.name == "NAS"
    assert args.min_capacity_tb == 16
    assert args.max_price_per_tb == Decimal("20")
    assert args.media_types == ["rotational"]
    assert args.conditions == ["new", "used"]
    assert args.sources == ["diskprices", "ebay", "leboncoin"]


def test_auth_rejects_when_allow_list_is_set() -> None:
    settings = Settings(telegram_allowed_user_ids=[123])
    assert is_authorized(settings, 123)
    assert not is_authorized(settings, 456)


def test_parse_alert_id_and_price() -> None:
    assert _alert_id_and_price("1 18,5") == (1, Decimal("18.5"))
    assert _alert_id_and_price("1 none") == (1, None)
    assert _alert_id_and_price("bad") is None
