# walrus-cli

`walrus-cli` is a client for [`walrus`](https://github.com/lukechampine/walrus).
It is currently geared towards watch-only wallets used in tandem with a Ledger
Nano S hardware wallet. Support for "hot" seed-based wallets may be added in the
future.

## Setup

You will need a synchronized `walrus` server running in watch-only mode. You
will also need a Ledger Nano S hardware wallet with the
[Sia app](https://github.com/LedgerHQ/nanos-app-sia) installed. The app must
be open on the device when `walrus-cli` commands are run.


## Generating an Address

Run `walrus-cli addr` to generate a new address from your seed. The address will
be added to the watch-only `walrus` server, so any coins you send to the address
will appears as spendable outputs.


## Creating a Transaction

Use the `walrus-cli txn` command to construct a transaction. Transactions are
specified in comma-sparated address:amount pairs; for example, to send 100 SC to
one address and 0.1 SC to another, you would write:

```
$ export DEST_ADDR_1=2ce86e0f5c4b282b51508a798ab8f1091c1cfcc0ee0feaa840e514f37af8dd2f3078fa83f125
$ export DEST_ADDR_2=62e4b26fd772a25029b92b4f06e87202b97bd9a214ff458154bb96e350fda2991b4afb1ff8ed
$ walrus-cli txn $DEST_ADDR_1:100,$DEST_ADDR_2:0.1 txn.json
```

`walrus-cli` will figure out which UTXOs to use, select an appropriate fee, and
send any change back to an address you control. The resulting transaction will
be written to disk as JSON.


## Signing a Transaction

Run `walrus-cli sign txn.json` to sign the transaction stored in `txn.json`. The
details of the transaction will appear on your Nano S screen, and you will be
prompted to approve signatures for each input you are spending. Unfortunately,
you must scroll through the transaction details for *each* signature. The signed
transaction will be written to `txn-signed.json`.


## Broadcasting a Transaction

Broadcasting a transaction is as simple as `walrus-cli broadcast txn-signed.json`.


## All Together Now

For convenience, you can merge these three steps (creating, signing,
broadcasting) into one. Just pass the `--sign` and `--broadcast` flags to the
`txn` command (or just `--sign` if you don't want to broadcast immediately). The
transaction will be constructed, signed, and broadcast without ever touching
disk. You can also pass the `--broadcast` flag to the `sign` command for the
same effect.
