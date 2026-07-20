UPDATE app_config SET value = 'https://www.dealabs.com/rss/hot' WHERE key = 'DEALABS_RSS_URLS';
DELETE FROM app_config WHERE key = 'IDEALO_FEED_URLS';
DELETE FROM app_config WHERE key = 'LEDENICHEUR_FEED_URLS';
DELETE FROM app_config WHERE key = 'LEBONCOIN_FEED_URLS';
