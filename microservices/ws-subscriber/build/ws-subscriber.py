import asyncio
import hashlib
import json
import sys
import time
import websocket
import hmac
import os

def sign_ws_request(event, account, type, requestId, secret_key):
    signature_hash = hmac.new(secret_key.encode(),
                              f'{event}{account}{type}{requestId}'.encode(),hashlib.sha384).hexdigest()
    return signature_hash

try:
    import thread
except ImportError:
    import _thread as thread

async def main():

    data={}
    data["event"] = event
    data["types"] = [type]
    data["requestId"] = requestId
    data["username"] = username
    data["password"] = sign_ws_request(event,account,type,requestId,secret_key)
    data["account"] = account
    data["useApiKeyForAuth"] = True
    data["isInitialSnap"] = True

    def on_message(ws, message):
        print(json.dumps(json.loads(message),indent=4))

    def on_error(ws, error):
        print(error)

    def on_close(ws,arg1,arg2):
        print("### Exiting websocket test... ###")

    def on_open(ws):
        ws.send(json.dumps(data, separators=(',', ':')))

        def run(*args):
            while True:
                time.sleep(25)
                ws.send(json.dumps({"heartbeat":25}, separators=(',', ':')))
        thread.start_new_thread(run, ())
    websocket.enableTrace(True)

    base_url = f"wss://{url}/wsapi"
    ws = websocket.WebSocketApp(base_url,
                                on_message=on_message,
                                on_error=on_error,
                                on_close=on_close)
    ws.on_open = on_open
    ws.run_forever()

if __name__ == "__main__":

    #get from kubernetes manifest env
    url = os.environ['WS_URL']
    username = os.environ['WS_USERNAME']
    password = os.environ['WS_PASSWORD']
    account = os.environ['USER_ACCOUNT_ID']
    token = os.environ['TOKEN']
    secret_key=os.environ['SECRET_KEY']

    event="S"
    requestId="Test123"
    type=6

    if "CONTINUE" in os.environ:
       continue_running = int(os.environ['CONTINUE'])
       print("enabled to run") 
       if continue_running == 0:
          print("exiting ...",continue_running)
          sys.exit()
       else:
          print("(main) running websocket subscriber ...")
          asyncio.run(main())
    else:
       print("unsubscribing ...",i)
       sys.exit()
