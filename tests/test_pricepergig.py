from decimal import Decimal

from diskcount.sources.pricepergig import parse_pricepergig_api


def test_parse_pricepergig_api() -> None:
    payload = [
        {
            "id": 123,
            "name": "Seagate Exos X18 18TB Internal 3.5 HDD SATA",
            "capacity_gb": 18000,
            "price": 330,
            "price_per_tb": 18.3333,
            "currency": "€",
            "condition": "New",
            "technology": "HDD",
            "interface": "SATA",
            "form_factor": 'Internal 3.5"',
            "marketplace": "amazon.fr",
            "url": "https://www.amazon.fr/dp/B0ABCDEFGH?tag=ppg09f-20",
        }
    ]

    deals = parse_pricepergig_api(payload)

    assert len(deals) == 1
    assert deals[0].source == "pricepergig"
    assert deals[0].external_id == "123"
    assert deals[0].price_eur == Decimal("330.00")
    assert deals[0].price_per_tb == Decimal("18.33")
    assert deals[0].capacity_tb == Decimal("18.000")
    assert deals[0].condition == "new"
    assert deals[0].media_type == "rotational"
    assert deals[0].drive_category == "internal_3_5"
    assert deals[0].interfaces == ("sata",)
    assert deals[0].url.endswith("?tag=ppg09f-20")
