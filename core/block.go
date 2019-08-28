package acb2fb3994c274446f5dd4d8397d2f73ad68f32f649e2577c23877f3a4d7e1a05

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/lengzhao/govm/conf"
	"github.com/lengzhao/govm/database"
	"github.com/lengzhao/govm/runtime"
	"github.com/lengzhao/govm/wallet"
)

// StBlock StBlock
type StBlock struct {
	Block
	Key          Hash
	sign         []byte
	TransList    []Hash
	PreHashpower uint8
}

// TReliability Reliability of block
type TReliability struct {
	Key         Hash   `json:"key,omitempty"`
	Previous    Hash   `json:"previous,omitempty"`
	Time        uint64 `json:"time,omitempty"`
	Index       uint64 `json:"index,omitempty"`
	Reliability []byte `json:"reliability,omitempty"`
	rel         big.Int
	HashPower   uint8 `json:"hash_power,omitempty"`
	Miner       bool  `json:"miner,omitempty"`
}
type dbReliability struct{}

// NewBlock new block
/*
	1. NewBlock
	2. SetTransList
	3. update StBlock.Size,PreCheckSum,HashPower,Parent...
	4. GetSignData
	5. SetSign
	6. Output
*/
func NewBlock(chain uint64, producer Address) *StBlock {
	var preHashPower uint64
	var blockInterval uint64
	var pStat BaseInfo
	out := new(StBlock)
	getDataFormDB(chain, dbStat{}, []byte{StatBaseInfo}, &pStat)
	getDataFormDB(chain, dbStat{}, []byte{StatHashPower}, &preHashPower)
	getDataFormDB(chain, dbStat{}, []byte{StatBlockInterval}, &blockInterval)
	out.PreHashpower = uint8(preHashPower)

	if preHashPower == 0 {
		preHashPower = 5
	}
	hp := getHashPower(pStat.Key)
	if uint64(hp) > preHashPower+10 {
		preHashPower++
	} else if uint64(hp) < preHashPower+5 {
		preHashPower--
	}

	if preHashPower == 0 {
		preHashPower++
	}

	if pStat.ID == 1 && chain > 1 {
		pStat.Time = pStat.Time + blockSyncMax + blockSyncMin + baseBlockInterval
	} else {
		pStat.Time += blockInterval
	}

	out.HashPower = uint8(preHashPower)
	out.Previous = pStat.Key
	out.Producer = producer
	out.Time = pStat.Time

	out.Chain = chain
	out.Index = pStat.ID + 1

	out.TransList = make([]Hash, 0)

	if pStat.Chain > 1 {
		var key Hash
		var tmp BlockInfo
		getDataFormLog(chain/2, logBlockInfo{}, runtime.Encode(pStat.ParentID+1), &key)
		getDataFormLog(chain/2, logBlockInfo{}, key[:], &tmp)
		if !key.Empty() && out.Time > tmp.Time && out.Time-tmp.Time > blockSyncMin {
			out.Parent = key
		} else {
			getDataFormLog(chain/2, logBlockInfo{}, runtime.Encode(pStat.ParentID), &key)
			out.Parent = key
		}
	}
	if pStat.LeftChildID > 0 {
		var key Hash
		var tmp BlockInfo
		getDataFormLog(2*chain, logBlockInfo{}, runtime.Encode(pStat.LeftChildID+1), &key)
		getDataFormLog(2*chain, logBlockInfo{}, key[:], &tmp)
		if !key.Empty() && out.Time > tmp.Time && out.Time-tmp.Time > blockSyncMin {
			out.LeftChild = key
		} else {
			getDataFormLog(2*chain, logBlockInfo{}, runtime.Encode(pStat.LeftChildID), &key)
			out.LeftChild = key
		}
	}
	if pStat.RightChildID > 0 {
		var key Hash
		var tmp BlockInfo
		getDataFormLog(2*chain+1, logBlockInfo{}, runtime.Encode(pStat.RightChildID+1), &key)
		getDataFormLog(2*chain+1, logBlockInfo{}, key[:], &tmp)
		if !key.Empty() && out.Time > tmp.Time && out.Time-tmp.Time > blockSyncMin {
			out.RightChild = key
		} else {
			getDataFormLog(2*chain+1, logBlockInfo{}, runtime.Encode(pStat.RightChildID), &key)
			out.RightChild = key
		}
	}

	return out
}

func getTransHash(t1, t2 Hash) Hash {
	hashKey := Hash{}
	data := append(t1[:], t2[:]...)
	hash := runtime.GetHash(data)
	runtime.Decode(hash, &hashKey)
	return hashKey
}

// SetTransList SetTransList
func (b *StBlock) SetTransList(list []Hash) {
	n := len(list)
	if n == 0 {
		b.TransListHash = Hash{}
		return
	}
	b.TransList = make([]Hash, n)
	for i, t := range list {
		b.TransList[i] = t
	}
	tmpList := list
	for len(tmpList) > 1 {
		n := len(tmpList)
		if n%2 != 0 {
			tmpList = append(tmpList, Hash{})
			n++
			// log.Println("list number++:", n)
		}
		for i := 0; i < n/2; i++ {
			tmpList[i] = getTransHash(tmpList[2*i], tmpList[2*i+1])
		}
		tmpList = tmpList[:n/2]
	}
	b.TransListHash = tmpList[0]
	// log.Printf("TransListHash:%x", b.TransListHash)
}

//GetSignData GetSignData
func (b *StBlock) GetSignData() []byte {
	b.Nonce++
	data := runtime.Encode(b.Block)
	data = append(data, b.streamTransList()...)
	return data
}

// SetSign SetSign
func (b *StBlock) SetSign(sign []byte) error {
	//b.SignLen = uint16(len(sign))
	b.sign = sign
	return nil
}

func (b *StBlock) streamTransList() []byte {
	var out []byte
	for _, t := range b.TransList {
		out = append(out, t[:]...)
	}
	//log.Printf("streamTransList.chain:%d,trans:%d,%x\n", b.Chain, len(b.TransList), out)
	return out
}

// Output Output
func (b *StBlock) Output() []byte {
	data := make([]byte, 1, 1000)
	data[0] = uint8(len(b.sign))
	data = append(data, b.sign...)
	data = append(data, runtime.Encode(b.Block)...)
	data = append(data, b.streamTransList()...)
	k := runtime.GetHash(data)
	runtime.Decode(k, &b.Key)
	return data
}

// GetTransList GetTransList
func (b *StBlock) GetTransList() []string {
	var out []string
	for _, key := range b.TransList {
		keyStr := hex.EncodeToString(key[:])
		out = append(out, keyStr)
	}
	return out
}

// DecodeBlock decode data and check sign, check hash
func DecodeBlock(data []byte) *StBlock {
	out := new(StBlock)
	out.sign = data[1 : data[0]+1]
	bData := data[data[0]+1:]
	n := runtime.Decode(bData, &out.Block)
	stream := bData[n:]
	if len(stream)%HashLen != 0 {
		return nil
	}
	if out.Index < 1 {
		return nil
	}
	if out.Time > uint64(time.Now().Unix()+5)*1000 {
		return nil
	}

	rst := wallet.Recover(out.Producer[:], out.sign, bData)
	if !rst {
		log.Println("fail to recover block")
		return nil
	}
	h := runtime.GetHash(data)
	runtime.Decode(h, &out.Key)
	transList := make([]Hash, len(stream)/HashLen)
	for i := 0; i < len(transList); i++ {
		n = runtime.Decode(stream, &transList[i])
		stream = stream[n:]
	}
	listKey := Hash{}
	copy(listKey[:], out.TransListHash[:])

	out.SetTransList(transList)
	if listKey != out.TransListHash {
		return nil
	}

	return out
}

// GetReliability get block reliability
func (b *StBlock) GetReliability() TReliability {
	const (
		WeightOfHashPower = 10000
	)
	var power uint
	var selfRel TReliability
	var miner Miner

	// getDataFormDB(b.Chain, dbReliability{}, b.Previous[:], &preRel)
	getDataFormDB(b.Chain, dbMining{}, runtime.Encode(b.Index), &miner)

	// hp := getHashPower(b.Key)
	// if hp < preRel.HashPower {
	// 	return selfRel
	// }
	// power += uint(hp)
	// power += uint(preRel.HashPower)
	for i := 0; i < minerNum; i++ {
		if miner.Miner[i] == b.Producer {
			power += uint(minerNum-i) + 5
			break
		}
	}
	// selfRel.Reliability = power * WeightOfHashPower
	// selfRel.Reliability += preRel.Reliability * (depositCycle - 1)
	// selfRel.Reliability = selfRel.Reliability / depositCycle
	// selfRel.Reliability = b.Key
	var rel big.Int
	rel.SetBytes(b.Key[:])
	selfRel.rel.Rsh(&rel, power)
	selfRel.Reliability = selfRel.rel.Bytes()

	selfRel.Key = b.Key
	selfRel.Index = b.Index
	selfRel.Previous = b.Previous
	selfRel.HashPower = b.HashPower
	selfRel.Time = b.Time

	return selfRel
}

// Cmp compares x and y and returns:
//
//   +1 if x <=  y
//   -1 if x >  y
func (x TReliability) Cmp(y TReliability) int {
	if x.rel.Cmp(&y.rel) <= 0 {
		return 1
	}
	return -1
}

// SaveBlockReliability save block reliability
func SaveBlockReliability(chain uint64, key []byte, rb TReliability) {
	data, err := json.Marshal(rb)
	if err != nil {
		log.Fatal(err)
	}
	runtime.AdminDbSet(dbReliability{}, chain, key, data, 2<<50)
}

// GetBlockReliability get Reliability of block
func GetBlockReliability(chain uint64, key []byte) (cl TReliability) {
	stream, _ := runtime.DbGet(dbReliability{}, chain, key)
	if stream != nil {
		json.Unmarshal(stream, &cl)
		cl.rel.SetBytes(cl.Reliability)
	}
	// getDataFormDB(chain, dbReliability{}, key, &cl)

	return
}

// DeleteBlockReliability delete reliability of block
func DeleteBlockReliability(chain uint64, key []byte) {
	info := TReliability{}
	runtime.AdminDbSet(dbReliability{}, chain, key, runtime.Encode(info), 0)
}

// IsExistBlock Determine whether block exists
func IsExistBlock(chain uint64, key []byte) bool {
	return runtime.DbExist(dbBlockData{}, chain, key)
}

// WriteBlock write block data to database
func WriteBlock(chain uint64, data []byte) error {
	key := runtime.GetHash(data)
	exist := runtime.DbExist(dbBlockData{}, chain, key)
	if exist {
		return nil
	}

	return runtime.AdminDbSet(dbBlockData{}, chain, key, data, 2<<50)
}

func getDataFormLog(chain uint64, db interface{}, key []byte, out interface{}) {
	if chain == 0 {
		return
	}
	stream, _ := runtime.LogRead(db, chain, key)
	if stream != nil {
		runtime.Decode(stream, out)
	}
}

// ReadBlockData read block data
func ReadBlockData(chain uint64, key []byte) []byte {
	stream, _ := runtime.DbGet(dbBlockData{}, chain, key)
	return stream
}

// GetChainInfo get chain info
func GetChainInfo(chain uint64) *BaseInfo {
	var pStat BaseInfo
	getDataFormDB(chain, dbStat{}, []byte{StatBaseInfo}, &pStat)
	return &pStat
}

// GetLastBlockIndex get the index of last block
func GetLastBlockIndex(chain uint64) uint64 {
	var pStat BaseInfo
	getDataFormDB(chain, dbStat{}, []byte{StatBaseInfo}, &pStat)
	return pStat.ID
}

// GetTheBlockKey get block key,if index==0,return last key
func GetTheBlockKey(chain, index uint64) []byte {
	if chain == 0 {
		return nil
	}
	if index == 0 {
		index = GetLastBlockIndex(chain)
	}
	var key Hash
	getDataFormLog(chain, logBlockInfo{}, runtime.Encode(index), &key)
	if key.Empty() {
		return nil
	}
	return key[:]
}

// GetBlockTime get block time
func GetBlockTime(chain uint64) uint64 {
	var pStat BaseInfo
	getDataFormDB(chain, dbStat{}, []byte{StatBaseInfo}, &pStat)
	return pStat.Time
}

// ChainIsCreated return true when the chain is created
func ChainIsCreated(chain uint64) bool {
	var pStat BaseInfo
	getDataFormDB(chain/2, dbStat{}, []byte{StatBaseInfo}, &pStat)
	if chain%2 == 0 {
		return pStat.LeftChildID > 0
	}
	return pStat.RightChildID > 0
}

// IsMiner check miner
func IsMiner(chain, index uint64, user []byte) bool {
	var miner Miner
	var addr Address
	if len(user) != AddressLen {
		return false
	}
	runtime.Decode(user, &addr)
	getDataFormDB(chain, dbMining{}, runtime.Encode(index), &miner)

	for i := 0; i < minerNum; i++ {
		if miner.Miner[i] == addr {
			return true
		}
	}

	return false
}

// GetMinerInfo get miner info
func GetMinerInfo(chain, index uint64) Miner {
	var miner Miner
	var guerdon uint64
	getDataFormDB(chain, dbStat{}, []byte{StatGuerdon}, &guerdon)
	getDataFormDB(chain, dbMining{}, runtime.Encode(index), &miner)
	guerdon = 3*guerdon - 1
	for i := 0; i < minerNum; i++ {
		if miner.Cost[i] == 0 {
			miner.Cost[i] = guerdon
		}
	}

	return miner
}

// RunBlock run block
func RunBlock(chain uint64, key []byte) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("RunBlock error,chain %d, key %x, err:%s", chain, key, e)
		}
	}()
	c := conf.GetConf()
	err = database.OpenFlag(chain, key)
	if err != nil {
		log.Println("fail to open Flag,", err)
		return
	}
	defer database.Cancel(chain, key)

	param := runtime.Encode(chain)
	param = append(param, key...)
	runtime.RunApp(key, chain, c.CorePackName, nil, param, 2<<50, 0)
	database.Commit(chain, key)
	return nil
}

// CreateBiosTrans CreateBiosTrans
func CreateBiosTrans(chain uint64) {
	c := conf.GetConf()
	err := database.OpenFlag(chain, c.FirstTransName)
	if err != nil {
		log.Println("fail to open Flag,", err)
		return
	}
	defer database.Cancel(chain, c.FirstTransName)
	data, _ := runtime.DbGet(dbTransactionData{}, chain, c.FirstTransName)
	trans := DecodeTrans(data)

	appCode := trans.Data
	appName := runtime.GetHash(appCode)
	log.Printf("first app: %x\n", appName)
	appCode[6] = appCode[6] | AppFlagRun
	runtime.NewApp(chain, appName, appCode)
}