package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"gitlab.com/NebulousLabs/Sia/types"
)

type walrusClient struct {
	addr string
}

func (c walrusClient) req(method string, route string, data, resp interface{}) error {
	var body io.Reader
	if data != nil {
		js, _ := json.Marshal(data)
		body = bytes.NewReader(js)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://%v%v", c.addr, route), body)
	if err != nil {
		panic(err)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer io.Copy(ioutil.Discard, r.Body)
	defer r.Body.Close()
	if r.StatusCode != 200 {
		err, _ := ioutil.ReadAll(r.Body)
		return errors.New(string(err))
	}
	if resp == nil {
		return nil
	}
	return json.NewDecoder(r.Body).Decode(resp)
}

func (c walrusClient) get(route string, r interface{}) error     { return c.req("GET", route, nil, r) }
func (c walrusClient) post(route string, d, r interface{}) error { return c.req("POST", route, d, r) }
func (c walrusClient) put(route string, d interface{}) error     { return c.req("PUT", route, d, nil) }
func (c walrusClient) delete(route string) error                 { return c.req("DELETE", route, nil, nil) }

func (c *walrusClient) Balance() (bal types.Currency, err error) {
	err = c.get("/balance", &bal)
	return
}

func (c *walrusClient) AllAddresses() (addrs []types.UnlockHash, err error) {
	err = c.get("/addresses", &addrs)
	return
}

type seedAddressInfo struct {
	UnlockConditions types.UnlockConditions
	KeyIndex         uint64
}

func (c *walrusClient) AddressInfo(addr types.UnlockHash) (info seedAddressInfo, err error) {
	err = c.get("/addresses/"+addr.String(), &info)
	return
}

func (c *walrusClient) WatchAddress(info seedAddressInfo) error {
	return c.post("/addresses", info, new(types.UnlockHash))
}

func (c *walrusClient) Broadcast(txnSet []types.Transaction) error {
	return c.post("/broadcast", txnSet, nil)
}

// A seedUTXO is an unspent output owned by a seed-derived address.
type seedUTXO struct {
	ID               types.SiacoinOutputID  `json:"ID"`
	Value            types.Currency         `json:"value"`
	UnlockConditions types.UnlockConditions `json:"unlockConditions"`
	UnlockHash       types.UnlockHash       `json:"unlockHash"`
	KeyIndex         uint64                 `json:"keyIndex"`
}

func (c *walrusClient) UnspentOutputs() (utxos []seedUTXO, err error) {
	err = c.get("/utxos", &utxos)
	return
}

func (c *walrusClient) RecommendedFee() (fee types.Currency, err error) {
	err = c.get("/fee", &fee)
	return
}

func makeClient(addr string) *walrusClient {
	return &walrusClient{addr: addr}
}
