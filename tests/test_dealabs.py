from decimal import Decimal

from diskcount.sources.dealabs import parse_dealabs_feed


def test_parse_dealabs_rss_entry() -> None:
    feed = """
    <rss version="2.0">
      <channel>
        <item>
          <title>Disque dur Seagate Expansion 16 To HDD - 299 EUR</title>
          <link>https://www.dealabs.com/bons-plans/example</link>
          <guid>deal-1</guid>
          <description>Bon prix pour ce disque dur externe neuf.</description>
        </item>
      </channel>
    </rss>
    """
    deals = parse_dealabs_feed(feed)
    assert len(deals) == 1
    assert deals[0].source == "dealabs"
    assert deals[0].price_per_tb == Decimal("18.69")
    assert deals[0].media_type == "rotational"
    assert deals[0].condition == "new"
