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

url = "eqo-sit.com"
username="ngocdf1_qa_indi_7uxp@mailinator.com"
password="Eqonex@123456"
account="2661"
token="l1Q21BJzMZb39hWI"
secret_key="c6Z36Y2mckce7MWsGxi0HZ1n"
event="S"
requestId="Test123"
type=6

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

    continue_running = 0

    for i in range(10):
      print("debug> ",i) 
      print(os.environ)
      time.sleep(10)

    if "CONTINUE" in os.environ:
       continue_running = int(os.environ['CONTINUE'])
       for i in range(10):
        print("debug> ",i) 
        time.sleep(10)
       if continue_running == 0:
          print("exiting ...",continue_running)
          for i in range(10):
           print("debug> ",i) 
          sys.exit()
       else:
          print("(main) running websocket subscriber ...")
          asyncio.run(main())
    else:
       for i in range(10):
        print("debug> exiting ...",i)
        time.sleep(10)
       sys.exit()

