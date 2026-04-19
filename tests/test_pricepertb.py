from decimal import Decimal

from diskcount.sources.pricepertb import parse_pricepertb_html


def test_parse_pricepertb_table() -> None:
    html = """
    <table id="diskprices">
      <tbody>
        <tr class="disk">
          <td class="price-per-gb">€0,019</td>
          <td class="price-per-tb">€19,25</td>
          <td>€347</td>
          <td>18 TB</td>
          <td>2 years</td>
          <td>External 3.5&quot;</td>
          <td>HDD</td>
          <td>Used</td>
          <td class="name">
            <a href="https://www.amazon.fr/dp/B0ABCDEFGH?tag=PricePerTB00-20">WD 18 To Elements HDD USB 3.0</a>
          </td>
        </tr>
      </tbody>
    </table>
    """

    deals = parse_pricepertb_html(html)

    assert len(deals) == 1
    assert deals[0].source == "pricepertb"
    assert deals[0].external_id == "B0ABCDEFGH"
    assert deals[0].price_eur == Decimal("347.00")
    assert deals[0].price_per_tb == Decimal("19.25")
    assert deals[0].capacity_tb == Decimal("18")
    assert deals[0].condition == "used"
    assert deals[0].media_type == "rotational"
    assert deals[0].drive_category == "external_3_5"
    assert deals[0].interfaces == ("usb",)
