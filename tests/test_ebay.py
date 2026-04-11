from decimal import Decimal

from diskcount.sources.ebay import parse_ebay_search_response


def test_parse_ebay_search_response() -> None:
    payload = {
        "itemSummaries": [
            {
                "itemId": "v1|123|0",
                "title": "Seagate Exos X18 16 To HDD",
                "itemWebUrl": "https://www.ebay.fr/itm/123",
                "condition": "Occasion",
                "price": {"value": "280.00", "currency": "EUR"},
            },
            {
                "itemId": "v1|ignored|0",
                "title": "Seagate Exos X18 16 To HDD",
                "itemWebUrl": "https://www.ebay.fr/itm/ignored",
                "condition": "Occasion",
                "price": {"value": "280.00", "currency": "USD"},
            },
        ]
    }

    deals = parse_ebay_search_response(payload)

    assert len(deals) == 1
    assert deals[0].source == "ebay"
    assert deals[0].external_id == "v1|123|0"
    assert deals[0].price_eur == Decimal("280.00")
    assert deals[0].price_per_tb == Decimal("17.50")
    assert deals[0].condition == "used"
    assert deals[0].media_type == "rotational"
