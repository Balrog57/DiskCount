from decimal import Decimal

from diskcount.cli import _filter_deals
from diskcount.domain import Deal


def test_filter_deals_for_cli_list() -> None:
    deals = [
        Deal(
            source="diskprices",
            title="WD 16 To",
            url="https://example.com/16",
            price_eur=Decimal("320.00"),
            price_per_tb=Decimal("20.00"),
            capacity_tb=Decimal("16"),
            condition="new",
            media_type="rotational",
        ),
        Deal(
            source="diskprices",
            title="SSD 4 To",
            url="https://example.com/4",
            price_eur=Decimal("200.00"),
            price_per_tb=Decimal("50.00"),
            capacity_tb=Decimal("4"),
            condition="new",
            media_type="solid_state",
        ),
    ]

    filtered = _filter_deals(deals, Decimal("16"), Decimal("20"), "rotational", "new")

    assert len(filtered) == 1
    assert filtered[0].title == "WD 16 To"
