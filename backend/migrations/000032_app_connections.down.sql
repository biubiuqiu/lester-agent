-- Removes saved app grants only; it never changes external accounts/resources.
DROP TABLE app_connection_oauth_flows;
DROP TABLE app_connections;
