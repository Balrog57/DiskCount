from diskcount.sources.diskprices import parse_diskprices_html


def test_parse_diskprices_table() -> None:
    html = """
    <table>
      <tr>
        <th>Price per GB</th><th>Price per TB</th><th>Price</th><th>Capacity</th><th>Warranty</th>
        <th>Form Factor</th><th>Technology</th><th>Condition</th><th>Affiliate Link</th>
      </tr>
      <tr>
        <td>EUR0,020</td><td>EUR20,00</td><td>EUR320</td><td>16 TB</td><td>2 years</td>
        <td>External 3.5&quot;</td><td>HDD</td><td>New</td>
        <td><a href="https://www.amazon.fr/dp/B0ABCDEFGH">WD 16 To Elements</a></td>
      </tr>
    </table>
    """
    deals = parse_diskprices_html(html)
    assert len(deals) == 1
    assert deals[0].price_per_tb == 20
    assert deals[0].capacity_tb == 16
    assert deals[0].condition == "new"
    assert deals[0].media_type == "rotational"
    assert deals[0].external_id == "B0ABCDEFGH"
