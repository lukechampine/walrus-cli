package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gitlab.com/NebulousLabs/Sia/build"
	"gitlab.com/NebulousLabs/Sia/crypto"
	"gitlab.com/NebulousLabs/Sia/types"
	"golang.org/x/crypto/ssh/terminal"
	"lukechampine.com/flagg"
	"lukechampine.com/sialedger"
	"lukechampine.com/us/wallet"
	"lukechampine.com/walrus"
)

var (
	// to be supplied at build time
	githash   = "?"
	builddate = "?"
)

var (
	rootUsage = `Usage:
    walrus-cli [flags] [action]

Actions:
    seed            generate a seed
    balance         view current balance
    addresses       list addresses
    addr            generate an address
    txn             create a transaction
    sign            sign a transaction
    broadcast       broadcast a transaction
`
	versionUsage = rootUsage
	balanceUsage = `Usage:
    walrus-cli balance

Reports the current balance.
`
	seedUsage = `Usage:
    walrus-cli seed

Generates a random seed.
`
	addressesUsage = `Usage:
    walrus-cli addresses

Lists addresses known to the wallet.
`
	addrUsage = `Usage:
    walrus-cli addr
    walrus-cli addr [key index]

Generates an address. If no key index is provided, the lowest unused key index
is used. The address is added to the wallet's set of tracked addresses.
`
	txnUsage = `Usage:
    walrus-cli txn [outputs] [file]

Creates a transaction sending containing the provided outputs, which are
specified as a comma-separated list of address:value pairs, where value is
specified in SC. The inputs are be selected automatically, and a change
address is generated if needed.
`
	signUsage = `Usage:
    walrus-cli sign [txn]

Signs the inputs of the provided transaction that the wallet controls.
`
	broadcastUsage = `Usage:
    walrus-cli broadcast [txn]

Broadcasts the provided transaction.
`
)

func check(err error, ctx string) {
	if err != nil {
		log.Fatalf("%v: %v", ctx, err)
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func currencyUnits(c types.Currency) string {
	r := new(big.Rat).SetFrac(c.Big(), types.SiacoinPrecision.Big())
	sc := strings.TrimRight(r.FloatString(30), "0")
	return strings.TrimSuffix(sc, ".") + " SC"
}

func readTxn(filename string) types.Transaction {
	js, err := ioutil.ReadFile(filename)
	check(err, "Could not read transaction file")
	var txn types.Transaction
	err = json.Unmarshal(js, &txn)
	check(err, "Could not parse transaction file")
	return txn
}

func writeTxn(filename string, txn types.Transaction) {
	js, _ := json.MarshalIndent(txn, "", "  ")
	js = append(js, '\n')
	err := ioutil.WriteFile(filename, js, 0666)
	check(err, "Could not write transaction to disk")
}

func getDonationAddr(narwalAddr string) (types.UnlockHash, bool) {
	u, err := url.Parse(narwalAddr)
	if err != nil {
		return types.UnlockHash{}, false
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	path := strings.Split(u.Path, "/")
	if len(path) < 2 || path[len(path)-2] != "wallet" {
		return types.UnlockHash{}, false
	}
	path = append(path[:len(path)-2], "donations")
	u.Path = strings.Join(path, "/")
	resp, err := http.Get(u.String())
	if err != nil {
		return types.UnlockHash{}, false
	}
	defer resp.Body.Close()
	defer ioutil.ReadAll(resp.Body)
	var addr types.UnlockHash
	err = json.NewDecoder(resp.Body).Decode(&addr)
	return addr, err == nil
}

func getSeed() wallet.Seed {
	phrase := os.Getenv("WALRUS_SEED")
	if phrase != "" {
		fmt.Println("Using WALRUS_SEED environment variable")
	} else {
		fmt.Print("Seed: ")
		pw, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			log.Fatal("Could not read seed phrase:", err)
		}
		fmt.Println()
		phrase = string(pw)
	}
	seed, err := wallet.SeedFromPhrase(phrase)
	if err != nil {
		log.Fatal(err)
	}
	return seed
}

func main() {
	log.SetFlags(0)
	var sign, broadcast bool // used by txn and sign commands

	rootCmd := flagg.Root
	apiAddr := rootCmd.String("a", "localhost:9380", "host:port that the walrus API is running on")
	hot := rootCmd.Bool("hot", false, "use a 'hot' seed-based wallet")
	rootCmd.Usage = flagg.SimpleUsage(rootCmd, rootUsage)
	versionCmd := flagg.New("version", versionUsage)
	seedCmd := flagg.New("seed", seedUsage)
	balanceCmd := flagg.New("balance", balanceUsage)
	addressesCmd := flagg.New("addresses", addressesUsage)
	addrCmd := flagg.New("addr", addrUsage)
	txnCmd := flagg.New("txn", txnUsage)
	txnCmd.BoolVar(&sign, "sign", false, "sign the transaction")
	txnCmd.BoolVar(&broadcast, "broadcast", false, "broadcast the transaction")
	changeAddrStr := txnCmd.String("change", "", "use this change address instead of generating a new one")
	signCmd := flagg.New("sign", signUsage)
	signCmd.BoolVar(&broadcast, "broadcast", false, "broadcast the transaction (if true, omit file)")
	broadcastCmd := flagg.New("broadcast", broadcastUsage)

	cmd := flagg.Parse(flagg.Tree{
		Cmd: rootCmd,
		Sub: []flagg.Tree{
			{Cmd: versionCmd},
			{Cmd: seedCmd},
			{Cmd: balanceCmd},
			{Cmd: addressesCmd},
			{Cmd: addrCmd},
			{Cmd: txnCmd},
			{Cmd: signCmd},
			{Cmd: broadcastCmd},
		},
	})
	args := cmd.Args()

	switch cmd {
	case rootCmd:
		if len(args) != 0 {
			cmd.Usage()
			return
		}
		fallthrough
	case versionCmd:
		log.Printf("walrus-cli v0.1.0\nCommit:     %s\nRelease:    %s\nGo version: %s %s/%s\nBuild Date: %s\n",
			githash, build.Release, runtime.Version(), runtime.GOOS, runtime.GOARCH, builddate)

	case seedCmd:
		if len(args) != 0 {
			cmd.Usage()
			return
		}
		fmt.Println(wallet.NewSeed())

	case balanceCmd:
		if len(args) != 0 {
			cmd.Usage()
			return
		}
		c := walrus.NewClient(*apiAddr)
		bal, err := c.Balance(true)
		check(err, "Could not get balance")
		fmt.Println(currencyUnits(bal))

	case addressesCmd:
		if len(args) != 0 {
			cmd.Usage()
			return
		}
		c := walrus.NewClient(*apiAddr)
		addrs, err := c.Addresses()
		check(err, "Could not get address list")
		if len(addrs) == 0 {
			fmt.Println("No addresses.")
		} else {
			for _, addr := range addrs {
				fmt.Println(addr)
			}
		}

	case addrCmd:
		if len(args) > 1 {
			cmd.Usage()
			return
		}
		c := walrus.NewClient(*apiAddr)
		var index uint64
		var err error
		if len(args) == 0 {
			index, err = c.SeedIndex()
			check(err, "Could not get next seed index")
			fmt.Printf("No index specified; using lowest unused index (%v)\n", index)
		} else {
			index, err = strconv.ParseUint(args[0], 10, 32)
			check(err, "Invalid index")
		}
		var pubkey types.SiaPublicKey
		if *hot {
			seed := getSeed()
			pubkey = seed.PublicKey(index)
			fmt.Println("Derived address from seed:")
			fmt.Println("    " + wallet.StandardAddress(pubkey).String())
		} else {
			nanos, err := sialedger.OpenNanoS()
			check(err, "Could not connect to Nano S")
			fmt.Printf("Please verify and accept the prompt on your device to generate address #%v.\n", index)
			_, pubkey, err = nanos.GetAddress(uint32(index), false)
			check(err, "Could not generate address")
			fmt.Println("Compare the address displayed on your device to the address below:")
			fmt.Println("    " + wallet.StandardAddress(pubkey).String())
		}

		// check for duplicate
		addrInfo, err := c.AddressInfo(wallet.StandardAddress(pubkey))
		if err == nil && addrInfo.KeyIndex == index {
			fmt.Println(`The server reported that it is already tracking this address. No further
action is needed. Please be aware that reusing addresses can compromise
your privacy.`)
			break
		}

		fmt.Print("Press ENTER to add this address to your wallet, or Ctrl-C to cancel.")
		bufio.NewReader(os.Stdin).ReadLine()
		err = c.AddAddress(wallet.SeedAddressInfo{
			UnlockConditions: wallet.StandardUnlockConditions(pubkey),
			KeyIndex:         index,
		})
		check(err, "Could not add address to wallet")
		fmt.Println("Address added successfully.")

	case txnCmd:
		if !((len(args) == 2) || (len(args) == 1 && broadcast)) {
			cmd.Usage()
			return
		}
		// parse outputs
		pairs := strings.Split(args[0], ",")
		outputs := make([]types.SiacoinOutput, len(pairs))
		var recipSum types.Currency
		for i, p := range pairs {
			addrAmount := strings.Split(p, ":")
			if len(addrAmount) != 2 {
				check(errors.New("outputs must be specified in addr:amount pairs"), "Could not parse outputs")
			}
			err := outputs[i].UnlockHash.LoadString(strings.TrimSpace(addrAmount[0]))
			check(err, "Invalid destination address")
			amount, ok := new(big.Rat).SetString(strings.TrimSpace(addrAmount[1]))
			if !ok {
				_, err := strconv.ParseFloat(strings.TrimSpace(addrAmount[1]), 64)
				check(err, "Invalid destination amount")
			}
			outputs[i].Value = types.SiacoinPrecision.MulRat(amount)
			recipSum = recipSum.Add(outputs[i].Value)
		}

		// if using a narwal server, compute donation
		var donation types.Currency
		donationAddr, ok := getDonationAddr(*apiAddr)
		if ok {
			// donation is max(1%, 10SC)
			donation = recipSum.MulRat(big.NewRat(1, 100))
			if tenSC := types.SiacoinPrecision.Mul64(10); donation.Cmp(tenSC) < 0 {
				donation = tenSC
			}
		}

		// fund transaction
		c := walrus.NewClient(*apiAddr)
		utxos, err := c.UnspentOutputs(false)
		check(err, "Could not get utxos")
		inputs := make([]wallet.ValuedInput, len(utxos))
		for i, o := range utxos {
			info, err := c.AddressInfo(o.UnlockHash)
			check(err, "Could not get address info")
			inputs[i] = wallet.ValuedInput{
				SiacoinInput: types.SiacoinInput{
					ParentID:         o.ID,
					UnlockConditions: info.UnlockConditions,
				},
				Value: o.Value,
			}
		}
		feePerByte, err := c.RecommendedFee()
		check(err, "Could not get recommended transaction fee")
		used, fee, change, ok := wallet.FundTransaction(recipSum.Add(donation), feePerByte, inputs)
		if !ok {
			// couldn't afford transaction with donation; try funding without
			// donation and "donate the change" instead
			used, fee, change, ok = wallet.FundTransaction(recipSum, feePerByte, inputs)
			if !ok {
				check(errors.New("insufficient funds"), "Could not create transaction")
			}
			donation = change
			change = types.ZeroCurrency
		}
		if !donation.IsZero() {
			outputs = append(outputs, types.SiacoinOutput{
				UnlockHash: donationAddr,
				Value:      donation,
			})
		}

		// we may need these once, twice, or not at all, depending on whether
		// we're signing and whether we need a change output; regardless, we
		// only want to fetch them once.
		var nanos *sialedger.NanoS
		var seed wallet.Seed

		// get change address
		if !change.IsZero() {
			var changeAddr types.UnlockHash
			if *changeAddrStr != "" {
				err = changeAddr.LoadString(*changeAddrStr)
				check(err, "Could not parse change address")
			} else {
				var pubkey types.SiaPublicKey
				fmt.Println("This transaction requires a 'change output' that will send excess coins back to your wallet.")
				index, err := c.SeedIndex()
				check(err, "Could not get next seed index")
				if *hot {
					seed = getSeed()
					pubkey = seed.PublicKey(index)
					fmt.Println("Derived address from seed:")
					fmt.Println("    " + wallet.StandardAddress(pubkey).String())
				} else {
					fmt.Println("Please verify and accept the prompt on your device to generate a change address.")
					fmt.Println("(You may use the --change flag to specify a change address in advance.)")
					nanos, err = sialedger.OpenNanoS()
					check(err, "Could not connect to Nano S")
					_, pubkey, err = nanos.GetAddress(uint32(index), false)
					check(err, "Could not generate address")
					fmt.Println("Compare the address displayed on your device to the address below:")
					fmt.Println("    " + wallet.StandardAddress(pubkey).String())
				}
				fmt.Print("Press ENTER to add this address to your wallet, or Ctrl-C to cancel.")
				bufio.NewReader(os.Stdin).ReadLine()
				err = c.AddAddress(wallet.SeedAddressInfo{
					UnlockConditions: wallet.StandardUnlockConditions(pubkey),
					KeyIndex:         index,
				})
				check(err, "Could not add address to wallet")
				fmt.Println("Change address added successfully.")
				fmt.Println()
				changeAddr = wallet.StandardAddress(pubkey)
			}
			outputs = append(outputs, types.SiacoinOutput{
				Value:      change,
				UnlockHash: changeAddr,
			})
		}
		txn := types.Transaction{
			SiacoinInputs:  make([]types.SiacoinInput, len(used)),
			SiacoinOutputs: outputs,
			MinerFees:      []types.Currency{fee},
		}
		var inputSum types.Currency
		for i, in := range used {
			txn.SiacoinInputs[i] = in.SiacoinInput
			inputSum = inputSum.Add(in.Value)
		}
		check(err, "Could not write transaction to disk")
		fmt.Println("Transaction summary:")
		fmt.Printf("- %v input%v, totalling %v\n", len(used), plural(len(used)), currencyUnits(inputSum))
		fmt.Printf("- %v recipient%v, totalling %v\n", len(pairs), plural(len(pairs)), currencyUnits(recipSum))
		if !donation.IsZero() {
			fmt.Printf("- A donation of %v to the narwal server\n", currencyUnits(donation))
		}
		fmt.Printf("- A miner fee of %v, which is %v/byte\n", currencyUnits(fee), currencyUnits(feePerByte))
		if !change.IsZero() {
			fmt.Printf("- A change output, sending %v back to your wallet\n", currencyUnits(change))
		}
		fmt.Println()

		if sign {
			if *hot {
				if seed == (wallet.Seed{}) {
					seed = getSeed()
				}
				err := signFlowHot(c, seed, &txn)
				check(err, "Could not sign transaction")
			} else {
				if nanos == nil {
					nanos, err = sialedger.OpenNanoS()
					check(err, "Could not connect to Nano S")
				}
				err := signFlow(c, nanos, &txn)
				check(err, "Could not sign transaction")
			}
		} else {
			fmt.Println("Transaction has not been signed. You can sign it with the 'sign' command.")
		}

		if broadcast {
			err := broadcastFlow(c, txn)
			check(err, "Could not broadcast transaction")
			return
		}

		writeTxn(args[1], txn)
		if sign {
			fmt.Println("Wrote signed transaction to", args[1])
		} else {
			fmt.Println("Wrote unsigned transaction to", args[1])
		}

	case signCmd:
		if len(args) != 1 {
			cmd.Usage()
			return
		}
		txn := readTxn(args[0])
		c := walrus.NewClient(*apiAddr)
		if *hot {
			seed := getSeed()
			err := signFlowHot(c, seed, &txn)
			check(err, "Could not sign transaction")
		} else {
			nanos, err := sialedger.OpenNanoS()
			check(err, "Could not connect to Nano S")
			err = signFlow(c, nanos, &txn)
			check(err, "Could not sign transaction")
		}

		if broadcast {
			err := broadcastFlow(c, txn)
			check(err, "Could not broadcast transaction")
		} else {
			ext := filepath.Ext(args[0])
			signedPath := strings.TrimSuffix(args[0], ext) + "-signed" + ext
			writeTxn(signedPath, txn)
			fmt.Println("Wrote signed transaction to", signedPath+".")
			fmt.Println("You can now use the 'broadcast' command to broadcast this transaction.")
		}

	case broadcastCmd:
		if len(args) != 1 {
			cmd.Usage()
			return
		}
		err := broadcastFlow(walrus.NewClient(*apiAddr), readTxn(args[0]))
		check(err, "Could not broadcast transaction")
	}
}

func broadcastFlow(c *walrus.Client, txn types.Transaction) error {
	err := c.Broadcast([]types.Transaction{txn})
	if err != nil {
		return err
	}
	fmt.Println("Transaction broadcast successfully.")
	fmt.Println("Transaction ID:", txn.ID())
	return nil
}

func signFlow(c *walrus.Client, nanos *sialedger.NanoS, txn *types.Transaction) error {
	addrs, err := c.Addresses()
	check(err, "Could not get addresses")
	addrMap := make(map[types.UnlockHash]struct{})
	for _, addr := range addrs {
		addrMap[addr] = struct{}{}
	}
	sigMap := make(map[int]uint64)
	for _, in := range txn.SiacoinInputs {
		addr := in.UnlockConditions.UnlockHash()
		if _, ok := addrMap[addr]; ok {
			// get key index
			info, err := c.AddressInfo(addr)
			check(err, "Could not get address info")
			// add signature entry
			sig := wallet.StandardTransactionSignature(crypto.Hash(in.ParentID))
			txn.TransactionSignatures = append(txn.TransactionSignatures, sig)
			sigMap[len(txn.TransactionSignatures)-1] = info.KeyIndex
			continue
		}
	}
	if len(sigMap) == 0 {
		fmt.Println("Nothing to sign: transaction does not spend any outputs recognized by this wallet")
		return nil
	}
	// request signatures from device
	fmt.Println("Please verify the transaction details on your device. You should see:")
	for _, sco := range txn.SiacoinOutputs {
		fmt.Println("   ", sco.UnlockHash, "receiving", currencyUnits(sco.Value))
	}
	for _, fee := range txn.MinerFees {
		fmt.Println("    A miner fee of", currencyUnits(fee))
	}
	if len(sigMap) > 1 {
		fmt.Printf("Each signature must be completed separately, so you will be prompted %v times.\n", len(sigMap))
	}
	for sigIndex, keyIndex := range sigMap {
		fmt.Printf("Waiting for signature for input %v, key %v...", sigIndex, keyIndex)
		sig, err := nanos.SignTxn(*txn, uint16(sigIndex), uint32(keyIndex))
		check(err, "Could not get signature")
		txn.TransactionSignatures[sigIndex].Signature = sig[:]
		fmt.Println("Done")
	}
	return nil
}

func signFlowHot(c *walrus.Client, seed wallet.Seed, txn *types.Transaction) error {
	addrs, err := c.Addresses()
	check(err, "Could not get addresses")
	addrMap := make(map[types.UnlockHash]struct{})
	for _, addr := range addrs {
		addrMap[addr] = struct{}{}
	}
	sigMap := make(map[int]uint64)
	for _, in := range txn.SiacoinInputs {
		addr := in.UnlockConditions.UnlockHash()
		if _, ok := addrMap[addr]; ok {
			// get key index
			info, err := c.AddressInfo(addr)
			check(err, "Could not get address info")
			// add signature entry
			sig := wallet.StandardTransactionSignature(crypto.Hash(in.ParentID))
			txn.TransactionSignatures = append(txn.TransactionSignatures, sig)
			sigMap[len(txn.TransactionSignatures)-1] = info.KeyIndex
			continue
		}
	}
	if len(sigMap) == 0 {
		fmt.Println("Nothing to sign: transaction does not spend any outputs recognized by this wallet")
		return nil
	}
	fmt.Println("Please verify the transaction details:")
	for _, sco := range txn.SiacoinOutputs {
		fmt.Println("   ", sco.UnlockHash, "receiving", currencyUnits(sco.Value))
	}
	for _, fee := range txn.MinerFees {
		fmt.Println("    A miner fee of", currencyUnits(fee))
	}
	fmt.Print("Press ENTER to sign this transaction, or Ctrl-C to cancel.")
	bufio.NewReader(os.Stdin).ReadLine()

	// sign each TransactionSignature
	for sigIndex, keyIndex := range sigMap {
		sig := seed.SecretKey(keyIndex).SignHash(txn.SigHash(sigIndex, types.ASICHardforkHeight+1))
		txn.TransactionSignatures[sigIndex].Signature = sig
	}
	return nil
}
