from decimal import Decimal

from diskcount.sources.feed import parse_feed


def test_parse_configured_feed_source() -> None:
    feed = """
    <rss version="2.0">
      <channel>
        <item>
          <title>Annonce leboncoin disque dur 16 To HDD - 250 EUR</title>
          <link>https://www.leboncoin.fr/ad/example</link>
          <guid>lbc-1</guid>
          <description>Disque dur externe en tres bon etat.</description>
        </item>
      </channel>
    </rss>
    """

    deals = parse_feed(feed, "leboncoin", default_condition="used")

    assert len(deals) == 1
    assert deals[0].source == "leboncoin"
    assert deals[0].price_eur == Decimal("250.00")
    assert deals[0].price_per_tb == Decimal("15.62")
    assert deals[0].condition == "used"
    assert deals[0].media_type == "rotational"
