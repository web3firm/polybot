#!/usr/bin/env python3
"""
Derive Polymarket CLOB API credentials using py-clob-client

Usage:
  pip install py-clob-client
  python derive_creds.py

Then copy the credentials to your .env file.
"""

import os
from py_clob_client.client import ClobClient

# Configuration - Update these!
HOST = "https://clob.polymarket.com"
CHAIN_ID = 137  # Polygon

# Your wallet's private key (the one you use on Polymarket)
PRIVATE_KEY = os.getenv("WALLET_PRIVATE_KEY", "")

# Your funder address (if using proxy/email wallet)
# Leave empty if using direct EOA wallet
FUNDER = os.getenv("FUNDER_ADDRESS", "")

# Signature type: 0=EOA, 1=Email/Magic, 2=Proxy
SIG_TYPE = int(os.getenv("SIGNATURE_TYPE", "0"))

def main():
    if not PRIVATE_KEY:
        print("Error: Set WALLET_PRIVATE_KEY environment variable")
        print("Example: export WALLET_PRIVATE_KEY='0x...'")
        return

    print(f"Host: {HOST}")
    print(f"Chain ID: {CHAIN_ID}")
    print(f"Signature Type: {SIG_TYPE}")
    print(f"Funder: {FUNDER or '(same as signer)'}")
    print()

    try:
        if FUNDER:
            client = ClobClient(
                HOST,
                key=PRIVATE_KEY,
                chain_id=CHAIN_ID,
                signature_type=SIG_TYPE,
                funder=FUNDER
            )
        else:
            client = ClobClient(
                HOST,
                key=PRIVATE_KEY,
                chain_id=CHAIN_ID,
                signature_type=SIG_TYPE
            )

        print("Deriving API credentials...")
        creds = client.create_or_derive_api_creds()
        
        print("\n" + "="*60)
        print("SUCCESS! Add these to your .env file:")
        print("="*60)
        print(f"CLOB_API_KEY={creds.api_key}")
        print(f"CLOB_API_SECRET={creds.api_secret}")
        print(f"CLOB_PASSPHRASE={creds.api_passphrase}")
        print("="*60)

        # Test the credentials
        print("\nTesting credentials...")
        client.set_api_creds(creds)
        
        try:
            balance = client.get_balance_allowance()
            print(f"✅ Balance: ${float(balance.get('balance', 0)) / 1_000_000:.2f} USDC")
        except Exception as e:
            print(f"⚠️ Balance check failed: {e}")

    except Exception as e:
        print(f"❌ Error: {e}")
        print("\nTroubleshooting:")
        print("1. Make sure your private key is correct")
        print("2. If using email login, you need the Magic wallet private key")
        print("3. Try signature_type=0 for direct wallet, 1 for email/Magic")

if __name__ == "__main__":
    main()
