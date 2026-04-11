from decimal import Decimal

from diskcount.parsing import normalize_condition, normalize_media_type, parse_capacity_tb, parse_price_eur


def test_parse_french_price() -> None:
    assert parse_price_eur("EUR19,36") == Decimal("19.36")
    assert parse_price_eur("1 299,99 EUR") == Decimal("1299.99")


def test_parse_capacity_to_tb() -> None:
    assert parse_capacity_tb("16 To") == Decimal("16")
    assert parse_capacity_tb("500 Go") == Decimal("0.5")


def test_normalization() -> None:
    assert normalize_condition("Reconditionne") == "used"
    assert normalize_condition("Neuf") == "new"
    assert normalize_media_type("M.2 NVMe SSD") == "solid_state"
    assert normalize_media_type("Disque dur 3.5 7200rpm") == "rotational"
