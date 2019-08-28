/*
 * govm p2p api
 *
 * govm的分布式节点间交互的api
 *
 * API version: 1.0.0
 * Generated by: Swagger Codegen (https://github.com/swagger-api/swagger-codegen.git)
 */

package api

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/lengzhao/govm/conf"
	core "github.com/lengzhao/govm/core"
	"github.com/lengzhao/govm/event"
	"github.com/lengzhao/govm/messages"
	"github.com/lengzhao/govm/runtime"
	"github.com/lengzhao/govm/wallet"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
)

// WalletInfo wallet info for new
type WalletInfo struct {
	WalletFile string `json:"wallet_file,omitempty"`
	Password   string `json:"password,omitempty"`
}

// RespWalletInfo response wallet info
type RespWalletInfo struct {
	WalletAddr string `json:"wallet_addr,omitempty"`
}

// StopFlag stop flag
var StopFlag chan bool

func init() {
	StopFlag = make(chan bool, 1)
}

// WalletPost new wallet
func WalletPost(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err)
		return
	}
	info := WalletInfo{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	if info.WalletFile == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "need wallet file.")
	}
	if info.Password == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "need password.")
	}
	conf.LoadWallet(info.WalletFile, info.Password)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	out := RespWalletInfo{}
	out.WalletAddr = hex.EncodeToString(conf.GetConf().WalletAddr)
	enc := json.NewEncoder(w)
	enc.Encode(out)
}

// Account account
type Account struct {
	Chain   uint64 `json:"chain,omitempty"`
	Address string `json:"address,omitempty"`
	Cost    uint64 `json:"cost,omitempty"`
}

// AccountGet get account of the address on the chain
func AccountGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	r.ParseForm()
	chainStr := vars["chain"]
	addrStr := r.Form.Get("address")
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	var addr []byte
	if addrStr == "" {
		c := conf.GetConf()
		addrStr = hex.EncodeToString(c.WalletAddr)
	}
	addr, err = hex.DecodeString(addrStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error address,must hex string"))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	out := Account{}
	out.Chain = chain
	out.Address = addrStr
	out.Cost = core.GetUserCoin(chain, addr)
	enc := json.NewEncoder(w)
	enc.Encode(out)
}

// TransMoveInfo move info
type TransMoveInfo struct {
	DstChain uint64 `json:"dst_chain,omitempty"`
	Cost     uint64 `json:"cost,omitempty"`
	Energy   uint64 `json:"energy,omitempty"`
	TransKey string `json:"trans_key,omitempty"`
}

// TransactionMovePost move cost to other chain
func TransactionMovePost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := TransMoveInfo{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Cost)
		return
	}
	cAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	trans := core.NewTransaction(chain, cAddr)
	trans.CreateMove(info.DstChain, info.Cost)
	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()
	key := trans.Key[:]

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = key
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	info.Energy = trans.Energy
	info.TransKey = hex.EncodeToString(key)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// TranferInfo tranfer info
type TranferInfo struct {
	Peer     string `json:"peer,omitempty"`
	Cost     uint64 `json:"cost,omitempty"`
	Energy   uint64 `json:"energy,omitempty"`
	TransKey string `json:"trans_key,omitempty"`
}

// TransactionTranferPost tranfer
func TransactionTranferPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := TranferInfo{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	if info.Cost == 0 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "error cost value,", info.Cost)
		return
	}
	dst, err := hex.DecodeString(info.Peer)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "error address of peer,", info.Peer, err)
		return
	}
	if len(dst) != wallet.AddressLength {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "error length of peer address,", info.Peer)
		return
	}
	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Cost)
		return
	}
	cAddr := core.Address{}
	dstAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	runtime.Decode(dst, &dstAddr)
	log.Printf("transfer,from:%x,to:%x,cost:%d\n", cAddr, dstAddr, info.Cost)
	trans := core.NewTransaction(chain, cAddr)
	trans.CreateTransfer(dstAddr, info.Cost)
	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()
	key := trans.Key[:]

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = key
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	info.Energy = trans.Energy
	info.TransKey = hex.EncodeToString(key)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// MinerInfo miner info of get
type MinerInfo struct {
	Index uint64 `json:"index,omitempty"`
	core.Miner
}

// TransactionMinerGet get miner info
func TransactionMinerGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	r.ParseForm()
	chainStr := vars["chain"]
	idStr := r.Form.Get("index")
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error index"))
		return
	}

	miner := core.GetMinerInfo(chain, id)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	out := MinerInfo{}
	out.Index = id
	out.Miner = miner
	enc := json.NewEncoder(w)
	enc.Encode(out)
}

// Miner miner info
type Miner struct {
	TagetChain uint64 `json:"taget_chain,omitempty"`
	Index      uint64 `json:"index,omitempty"`
	Address    string `json:"address,omitempty"`
	Cost       uint64 `json:"cost,omitempty"`
	Energy     uint64 `json:"energy,omitempty"`
}

// TransactionMinerPost register miner
func TransactionMinerPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := Miner{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	miner := core.GetMinerInfo(chain, info.Index)
	if miner.Cost[core.MinerNum-1] >= info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost,old miner:%x,cost:%d, self:%d\n", miner.Miner[core.MinerNum-1], miner.Cost[core.MinerNum-1], info.Cost)
		return
	}
	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Cost)
		return
	}
	for i := 0; i < core.MinerNum; i++ {
		if bytes.Compare(c.WalletAddr, miner.Miner[i][:]) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "you are the miner.chain:%d,index:%d\n", chain, info.Index)
			return
		}
	}

	cAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	trans := core.NewTransaction(chain, cAddr)
	trans.CreateRegisterMiner(info.TagetChain, info.Index, info.Cost)
	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()
	key := trans.Key[:]

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = key
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	info.Energy = trans.Energy
	info.Address = hex.EncodeToString(key)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// NewApp new app
type NewApp struct {
	Cost         uint64 `json:"cost,omitempty"`
	Energy       uint64 `json:"energy,omitempty"`
	CodePath     string `json:"code_path,omitempty"`
	IsPrivate    bool   `json:"is_private,omitempty"`
	EnableRun    bool   `json:"enable_run,omitempty"`
	EnableImport bool   `json:"enable_import,omitempty"`
	AppName      string `json:"app_name,omitempty"`
}

// TransactionNewAppPost new app
func TransactionNewAppPost(w http.ResponseWriter, r *http.Request) {
	defer func() {
		err := recover()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "error code:", err)
		}
	}()
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := NewApp{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Cost)
		return
	}
	var flag uint8
	if !info.IsPrivate {
		flag |= core.AppFlagPlublc
		log.Println("1. flag:", flag)
	}
	if info.EnableRun {
		flag |= core.AppFlagRun
		log.Println("2. flag:", flag)
	}
	if info.EnableImport {
		flag |= core.AppFlagImport
		log.Println("3. flag:", flag)
	}
	code, ln := core.CreateAppFromSourceCode(info.CodePath, flag)
	cAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	trans := core.NewTransaction(chain, cAddr)
	trans.CreateNewApp(code, ln)
	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()
	key := trans.Key[:]

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = key
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	codeHS := runtime.GetHash(code)
	if info.IsPrivate {
		codeHS = runtime.GetHash(append(codeHS, trans.User[:]...))
	}
	info.AppName = hex.EncodeToString(codeHS)
	info.Energy = trans.Energy
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// RunApp run app
type RunApp struct {
	Cost       uint64 `json:"cost,omitempty"`
	Energy     uint64 `json:"energy,omitempty"`
	AppName    string `json:"app_name,omitempty"`
	Param      string `json:"param,omitempty"`
	SaveResult bool   `json:"save_result,omitempty"`
}

// TransactionRunAppPost run app
func TransactionRunAppPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := RunApp{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	log.Println("run app:", info)
	var param []byte
	if len(info.Param) > 0 {
		param, err = hex.DecodeString(info.Param)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "error param, hope hex string,", err)
			return
		}
	}
	app := core.Hash{}
	{
		d, err := hex.DecodeString(info.AppName)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "error AppName, hope hex string,", err)
			return
		}
		runtime.Decode(d, &app)
	}
	if (app == core.Hash{}) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "error AppName, fail to decode,", info.AppName)
		return
	}

	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Cost)
		return
	}

	cAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	trans := core.NewTransaction(chain, cAddr)
	if info.SaveResult {
		trans.CreateRunApp(app, info.Cost, param)
	} else {
		trans.CreateRunApp(app, info.Cost, param)
	}

	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = trans.Key[:]
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(trans)
}

// AppLife app life
type AppLife struct {
	Energy  uint64 `json:"energy,omitempty"`
	AppName string `json:"app_name,omitempty"`
	Life    uint64 `json:"life,omitempty"`
}

// TransactionAppLifePost update app life
func TransactionAppLifePost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := AppLife{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	app := core.Hash{}
	d, err := hex.DecodeString(info.AppName)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "error param, hope hex string,", err)
		return
	}
	runtime.Decode(d, &app)

	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Energy {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Energy)
		return
	}

	cAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	trans := core.NewTransaction(chain, cAddr)
	trans.CreateUpdateAppLife(app, info.Life)

	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = trans.Key[:]
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	info.Energy = trans.Energy
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// TransactionAppInfoGet get app info
func TransactionAppInfoGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	r.ParseForm()
	keyStr := r.Form.Get("key")
	if keyStr == "" {
		keyStr = "cb2fb3994c274446f5dd4d8397d2f73ad68f32f649e2577c23877f3a4d7e1a05"
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	var key []byte
	key, err = hex.DecodeString(keyStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error key"))
		return
	}
	info := core.GetAppInfoOfChain(chain, key)
	if info == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("error key"))
		fmt.Fprintf(w, "chain:%d,key:%x", chain, key)
		return
	}
	log.Println("app info:", info)
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// TransInfo transaction info
type TransInfo struct {
	core.TransactionHead
	Others interface{}
}

// TransactionInfoGet get transaction info
func TransactionInfoGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	r.ParseForm()
	keyStr := r.Form.Get("key")
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	var key []byte
	key, err = hex.DecodeString(keyStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error key"))
		return
	}

	data := core.ReadTransactionData(chain, key)
	if len(data) == 0 {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("error key"))
		fmt.Fprintf(w, "chain:%d,key:%x", chain, key)
		return
	}
	trans := core.DecodeTrans(data)
	if trans == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error key"))
		return
	}

	info := TransInfo{}
	info.TransactionHead = trans.TransactionHead
	info.Others = core.DecodeOpsDataOfTrans(info.Ops, trans.Data)

	d, _ := json.Marshal(info.Others)
	log.Println("trans info:", info.Others, string(d))

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// BlockMinePost mine
func BlockMinePost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	msg := new(messages.Mine)
	msg.Chain = chain
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(msg)
}

// BlockInfoGet get block info
func BlockInfoGet(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	r.ParseForm()
	indexStr := r.Form.Get("index")
	keyStr := r.Form.Get("key")
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	var index uint64
	if indexStr != "" {
		index, err = strconv.ParseUint(indexStr, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("error index"))
			return
		}
	}
	var key []byte
	if keyStr != "" {
		key, err = hex.DecodeString(keyStr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("error key"))
			return
		}
	} else {
		key = core.GetTheBlockKey(chain, index)
	}
	data := core.ReadBlockData(chain, key)
	if len(data) == 0 {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("error key"))
		fmt.Fprintf(w, "chain:%d,key:%x", chain, key)
		return
	}
	block := core.DecodeBlock(data)

	//relb := core.GetBlockReliability(chain, key)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(block)
}

// BlockRollback rollback
func BlockRollback(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	r.ParseForm()
	indexStr := r.Form.Get("index")
	keyStr := r.Form.Get("key")
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	var index uint64
	if indexStr != "" {
		index, err = strconv.ParseUint(indexStr, 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("error index"))
			return
		}
	} else {
		index = core.GetLastBlockIndex(chain)
	}

	var key []byte
	if keyStr != "" {
		key, err = hex.DecodeString(keyStr)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("error key"))
			return
		}
	} else {
		key = core.GetTheBlockKey(chain, index)
	}

	msg := new(messages.Rollback)
	msg.Chain = chain
	msg.Key = key
	msg.Index = index
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// ChainNewInfo info of new chain
type ChainNewInfo struct {
	DstChain uint64 `json:"dst_chain,omitempty"`
	Cost     uint64 `json:"cost,omitempty"`
	Energy   uint64 `json:"energy,omitempty"`
	TransKey string `json:"trans_key,omitempty"`
}

// ChainNew new chain
func ChainNew(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := ChainNewInfo{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}
	if info.DstChain/2 != chain {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "error dst chain,", info.DstChain)
		return
	}
	c := conf.GetConf()
	coin := core.GetUserCoin(chain, c.WalletAddr)
	if coin < info.Cost {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "not enough cost.have:%d,hope:%d\n", coin, info.Cost)
		return
	}
	cAddr := core.Address{}
	runtime.Decode(c.WalletAddr, &cAddr)
	trans := core.NewTransaction(chain, cAddr)
	trans.CreateNewChain(info.DstChain, info.Cost)
	// if conf.DebugMod {
	// 	trans.Time = core.GetBlockTime(chain)
	// }
	if info.Energy > trans.Energy {
		trans.Energy = info.Energy
	}
	td := trans.GetSignData()
	sign := wallet.Sign(c.PrivateKey, td)
	if len(c.SignPrefix) > 0 {
		s := make([]byte, len(c.SignPrefix))
		copy(s, c.SignPrefix)
		sign = append(s, sign...)
	}
	trans.SetSign(sign)
	td = trans.Output()
	key := trans.Key[:]

	msg := new(messages.NewTransaction)
	msg.Chain = chain
	msg.Key = key
	msg.Data = td
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	info.Energy = trans.Energy
	info.TransKey = hex.EncodeToString(key)

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// EventInfo event
type EventInfo struct {
	Who   string
	Event string
	Param string
}

// EventPost mine
func EventPost(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	chainStr := vars["chain"]
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to read body of request,", err, chainStr)
		return
	}
	chain, err := strconv.ParseUint(chainStr, 10, 64)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("error chain"))
		return
	}
	info := EventInfo{}
	err = json.Unmarshal(data, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, "fail to Unmarshal body of request,", err)
		return
	}

	msg := new(messages.ChainEvent)
	msg.Chain = chain
	msg.Who = info.Who
	msg.Event = info.Event
	msg.Param = info.Param
	err = event.Send(msg)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "error:%s", err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	enc := json.NewEncoder(w)
	enc.Encode(info)
}

// SystemExit system exit
func SystemExit(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	StopFlag <- true
}
