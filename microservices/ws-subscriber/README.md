# Websocket subscriber to simulate loader traders listening via websockets

- Each  pod is a trader with a unique login
- Each pod subscribes with a given heartbeat listening over a long period while other load testing is in progress (e.g order cancels, creations, listings etc ...)
