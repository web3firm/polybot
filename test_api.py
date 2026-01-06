#!/usr/bin/env python3
import hmac, hashlib, base64, requests, time

API_KEY = "019b92d8-9200-7e83-9d00-15e7f2e0a591"
API_SECRET = "FEXPvHYkCZqhb3RFQnyxppXk6_uUIep7pa0C9V_E6mE="
PASSPHRASE = "294105b4b133c29a887cd933d37486cfca3a48b3a18ac89f17f399681f62d97b"

ts = str(int(time.time()))
msg = f"{ts}GET/balance-allowance"

print(f"Timestamp: {ts}")
print(f"Message: {msg}")

secret = base64.urlsafe_b64decode(API_SECRET)
sig = base64.urlsafe_b64encode(hmac.new(secret, msg.encode(), hashlib.sha256).digest()).decode()
print(f"Signature: {sig}")

headers = {
    'POLY_API_KEY': API_KEY,
    'POLY_SIGNATURE': sig,
    'POLY_TIMESTAMP': ts,
    'POLY_PASSPHRASE': PASSPHRASE
}
print(f"Headers: {headers}")

r = requests.get('https://clob.polymarket.com/balance-allowance?asset_type=COLLATERAL&signature_type=0',
                 headers=headers, timeout=10)
print(f"Status: {r.status_code}")
print(f"Response: {r.text}")
