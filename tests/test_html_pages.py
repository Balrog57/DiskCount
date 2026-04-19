from decimal import Decimal

from diskcount.sources.html_pages import parse_html_deals


def test_parse_json_ld_product_deal() -> None:
    html = """
    <script type="application/ld+json">
    {
      "@context": "https://schema.org",
      "@type": "Product",
      "name": "WD Red Plus 16 To HDD SATA",
      "sku": "wd-red-16",
      "offers": {
        "@type": "Offer",
        "price": "299.99",
        "priceCurrency": "EUR",
        "url": "/wd-red-16"
      }
    }
    </script>
    """

    deals = parse_html_deals(html, "idealo", "https://www.idealo.fr/example", default_condition="new")

    assert len(deals) == 1
    assert deals[0].source == "idealo"
    assert deals[0].external_id == "wd-red-16"
    assert deals[0].price_eur == Decimal("299.99")
    assert deals[0].price_per_tb == Decimal("18.75")
    assert deals[0].capacity_tb == Decimal("16")
    assert deals[0].condition == "new"
    assert deals[0].media_type == "rotational"
    assert deals[0].url == "https://www.idealo.fr/wd-red-16"


def test_parse_anchor_deal_fallback() -> None:
    html = """
    <article>
      <a href="/offer/1">Samsung 990 Pro 4 To NVMe SSD</a>
      <span>Prix: 219,99 EUR</span>
    </article>
    """

    deals = parse_html_deals(html, "ledenicheur", "https://ledenicheur.fr/search", default_condition="new")

    assert len(deals) == 1
    assert deals[0].source == "ledenicheur"
    assert deals[0].price_eur == Decimal("219.99")
    assert deals[0].price_per_tb == Decimal("55.00")
    assert deals[0].capacity_tb == Decimal("4")
    assert deals[0].condition == "new"
    assert deals[0].media_type == "solid_state"
    assert deals[0].url == "https://ledenicheur.fr/offer/1"
