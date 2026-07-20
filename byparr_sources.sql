INSERT INTO app_config (key, value) VALUES ('MINDFACTORY_URLS', 'https://www.mindfactory.de/HDD+SSD') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
INSERT INTO app_config (key, value) VALUES ('CDISCOUNT_URLS', 'https://www.cdiscount.com/informatique/disque-dur-ssd-interne/') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
INSERT INTO app_config (key, value) VALUES ('RAKUTEN_URLS', 'https://fr.shopping.rakuten.com/s/disque+dur') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
INSERT INTO app_config (key, value) VALUES ('BACKMARKET_URLS', 'https://www.back-market.fr/c/disques-durs') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
INSERT INTO app_config (key, value) VALUES ('COMPUTERUNIVERSE_URLS', 'https://www.computeruniverse.de/') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
INSERT INTO app_config (key, value) VALUES ('PROSHOP_URLS', 'https://www.proshop.de/') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
INSERT INTO app_config (key, value) VALUES ('FNAC_URLS', 'https://www.fnac.com/Disque-dur-Interne/shi66454/w-3') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
