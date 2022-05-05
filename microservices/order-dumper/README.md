# Order Dumper Service: Capture orders from the order kafka topic into a DB for later replay
============================================================================================

- Dump Orders from the main order kafka db into a local REDIS DB
- Allow selection and re-play of orders to the orders API (HTTP or FIX)
