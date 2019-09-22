package handler

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/lengzhao/govm/conf"
	core "github.com/lengzhao/govm/core"
	"github.com/lengzhao/govm/database"
	"github.com/lengzhao/govm/messages"
	"github.com/lengzhao/govm/runtime"
	"github.com/lengzhao/libp2p"
	"log"
	"runtime/debug"
	"time"
)

// MsgPlugin process p2p message
type MsgPlugin struct {
	*libp2p.Plugin
	net libp2p.Network
}

const (
	connTime     = "connect_time"
	keyOfVersion = "message_version"
	keyOfChain   = "chain_id"
	keyOfAddr    = "peer_address"
)

var network libp2p.Network

// Startup is called only once when the plugin is loaded
func (p *MsgPlugin) Startup(n libp2p.Network) {
	p.net = n
	network = n
}

// PeerConnect is called every time a PeerSession is initialized and connected
func (p *MsgPlugin) PeerConnect(s libp2p.Session) {
	s.SetEnv(connTime, fmt.Sprintf("%d", time.Now().Unix()))
}

var first = true

// Receive receive message
func (p *MsgPlugin) Receive(ctx libp2p.Event) error {
	switch msg := ctx.GetMessage().(type) {
	case *messages.ReqBlockInfo:
		log.Printf("<%x> ReqBlockInfo %d %d\n", ctx.GetPeerID(), msg.Chain, msg.Index)
		key := core.GetTheBlockKey(msg.Chain, msg.Index)
		if len(key) == 0 {
			log.Println("fail to get the key,index:", msg.Index, ",chain:", msg.Chain)
			return nil
		}
		rel := core.ReadBlockReliability(msg.Chain, key)
		resp := new(messages.BlockInfo)
		resp.Chain = msg.Chain
		resp.Index = msg.Index
		resp.Key = key
		resp.PreKey = rel.Previous[:]
		ctx.Reply(resp)
		return nil
	case *messages.BlockInfo:
		log.Printf("<%x> BlockKey %d %d,key:%x\n", ctx.GetPeerID(), msg.Chain, msg.Index, msg.Key)
		hp := getHashPower(msg.Key)
		if hp < 5 || hp > 250 {
			return nil
		}

		index := core.GetLastBlockIndex(msg.Chain)
		if index > msg.Index {
			key2 := core.GetTheBlockKey(msg.Chain, msg.Index+1)
			if len(key2) > 0 {
				ctx.Reply(&messages.BlockInfo{Chain: msg.Chain, Index: msg.Index + 1, Key: key2})
			}
		}
		if msg.Index > index+1 {
			ctx.Reply(&messages.ReqBlockInfo{Chain: msg.Chain, Index: index + 1})
		}
		if msg.Index+10 < index || index+50 < msg.Index {
			return nil
		}

		if core.IsExistBlock(msg.Chain, msg.Key) {
			log.Println("block exist:", msg.Index, ",chain:", msg.Chain, ",self:", index)

			k := core.Hash{}
			runtime.Decode(msg.Key, &k)
			setBlockToIDBlocks(msg.Chain, msg.Index, k, 1)

			rel := core.ReadBlockReliability(msg.Chain, msg.Key)
			if !rel.Ready {
				data := core.ReadBlockData(msg.Chain, msg.Key)
				if data == nil {
					ctx.Reply(&messages.ReqBlock{Chain: msg.Chain, Index: msg.Index, Key: msg.Key})
					return nil
				}

				processBlock(ctx, msg.Chain, msg.Key, data)
			}
			return nil
		}
		ctx.Reply(&messages.ReqBlock{Chain: msg.Chain, Index: msg.Index, Key: msg.Key})
		if msg.Index == index {
			key := core.GetTheBlockKey(msg.Chain, index)
			ctx.Reply(&messages.BlockInfo{Chain: msg.Chain, Index: index, Key: key})
		}
	case *messages.TransactionInfo:
		log.Printf("<%x> TransactionInfo %d %x\n", ctx.GetPeerID(), msg.Chain, msg.Key)
		if len(msg.Key) != core.HashLen {
			return nil
		}
		if core.IsExistTransaction(msg.Chain, msg.Key) {
			return nil
		}
		ctx.Reply(&messages.ReqTransaction{Chain: msg.Chain, Key: msg.Key})
	case *messages.ReqBlock:
		log.Printf("<%x> ReqBlock %d %x\n", ctx.GetPeerID(), msg.Chain, msg.Key)
		if len(msg.Key) == 0 {
			return nil
		}
		if msg.Index > 0 {
			key := core.GetTheBlockKey(msg.Chain, msg.Index)
			if bytes.Compare(key, msg.Key) != 0 {
				index := core.GetLastBlockIndex(msg.Chain)
				if index > msg.Index+5 {
					ctx.Reply(&messages.BlockInfo{Chain: msg.Chain, Index: msg.Index, Key: key})
					return nil
				}
			}
		}

		data := core.ReadBlockData(msg.Chain, msg.Key)
		if len(data) == 0 {
			log.Printf("not found.ReqBlock chain:%d index:%d key:%x\n", msg.Chain, msg.Index, msg.Key)
			return nil
		}
		ctx.Reply(&messages.BlockData{Chain: msg.Chain, Key: msg.Key, Data: data})
	case *messages.ReqTransaction:
		log.Printf("<%x> ReqTransaction %d %x\n", ctx.GetPeerID(), msg.Chain, msg.Key)
		data := core.ReadTransactionData(msg.Chain, msg.Key)
		if data == nil {
			log.Printf("not found the block:%x in chain:%d\n", msg.Key, msg.Chain)
			return nil
		}
		ctx.Reply(&messages.TransactionData{Chain: msg.Chain, Key: msg.Key, Data: data})
	case *messages.BlockData:
		log.Printf("<%x> BlockData %d %x\n", ctx.GetPeerID(), msg.Chain, msg.Key)
		if len(msg.Data) > 102400 {
			return nil
		}

		processBlock(ctx, msg.Chain, msg.Key, msg.Data)

	case *messages.TransactionData:
		log.Printf("<%x> TransactionData %d %x\n", ctx.GetPeerID(), msg.Chain, msg.Key)
		if len(msg.Key) != core.HashLen {
			return nil
		}
		processTransaction(msg.Chain, msg.Key, msg.Data)
	default:
		//log.Println("msg", ctx.GetPeerID(), msg)
		if first {
			first = false
			index := core.GetLastBlockIndex(1)
			index++
			if index == 1 {
				ctx.Reply(&messages.ReqTransaction{Chain: 1, Key: conf.GetConf().FirstTransName})
			} else {
				ctx.Reply(&messages.ReqBlockInfo{Chain: 1, Index: index})
			}
		}
	}

	return nil
}

func blockRun(chain uint64, key []byte) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("blockRun error,chain %d, key %x, err:%s", chain, key, e)
			log.Println(string(debug.Stack()))
		}
	}()

	c := conf.GetConf()
	err = database.OpenFlag(chain, key)
	if err != nil {
		log.Println("fail to open Flag,", err)
		f := database.GetLastFlag(chain)
		database.Cancel(chain, f)
		return
	}
	defer database.Cancel(chain, key)

	param := runtime.Encode(chain)
	param = append(param, key...)
	runtime.RunApp(key, chain, c.CorePackName, nil, param, 2<<50, 0)
	database.Commit(chain, key)

	return
}

func processBlock(ctx libp2p.Event, chain uint64, key, data []byte) (err error) {
	if getHashPower(key) < 5 {
		return nil
	}
	// 解析block
	block := core.DecodeBlock(data)
	if block == nil {
		log.Printf("error block,chain:%d,key:%x\n", chain, key)
		return nil
	}

	if bytes.Compare(key, block.Key[:]) != 0 {
		log.Printf("error block key,chain:%d,hope key:%x,key:%x\n", chain, key, block.Key[:])
		return nil
	}

	//first block
	if chain != block.Chain {
		if block.Chain != 0 {
			log.Printf("error chain of block,hope chain:%d,chain:%d,key:%x\n", chain, block.Chain, key)
			return nil
		}

		if chain > 1 {
			nIndex := core.GetLastBlockIndex(chain / 2)
			if nIndex < 1 {
				return nil
			}
		}
	}

	now := uint64(time.Now().Unix()) * 1000
	if block.Time > now+5000 {
		return nil
	}

	// 如果block比当前的链短了10个块，则丢弃
	nIndex := core.GetLastBlockIndex(chain)
	if nIndex > block.Index+10 {
		log.Printf("the block too old,chain:%d,key:%x,index:%d,hope > %d\n", chain, key, block.Index, nIndex-10)
		return nil
	}

	// block已经处理过了，忽略
	lKey := core.GetTheBlockKey(chain, block.Index)
	if lKey != nil && bytes.Compare(key, lKey) == 0 {
		return nil
	}

	// 将数据写入db
	core.WriteBlock(chain, data)
	var lost bool

	//前一块还不存在，下载
	if block.Index > 1 && !core.IsExistBlock(chain, block.Previous[:]) {
		ctx.Reply(&messages.ReqBlock{Chain: chain, Index: block.Index - 1, Key: block.Previous[:]})
		lost = true
	} else {
		setBlockToIDBlocks(chain, block.Index-1, block.Previous, 1)
	}

	if !block.Parent.Empty() && !core.IsExistBlock(chain/2, block.Parent[:]) {
		ctx.Reply(&messages.ReqBlock{Chain: chain / 2, Index: 0, Key: block.Parent[:]})
		lost = true
	}
	if !block.LeftChild.Empty() && !core.IsExistBlock(chain*2, block.LeftChild[:]) {
		ctx.Reply(&messages.ReqBlock{Chain: chain * 2, Index: 0, Key: block.LeftChild[:]})
		lost = true
	}
	if !block.RightChild.Empty() && !core.IsExistBlock(chain*2+1, block.RightChild[:]) {
		ctx.Reply(&messages.ReqBlock{Chain: chain*2 + 1, Index: 0, Key: block.RightChild[:]})
		lost = true
	}

	for _, t := range block.TransList {
		if core.IsExistTransaction(chain, t[:]) {
			continue
		}
		ctx.Reply(&messages.ReqTransaction{Chain: chain, Key: t[:]})
		lost = true
	}

	ib := core.ReadIDBlocks(chain, block.Index)
	for _, it := range ib.Items {
		if block.Key == it.Key {
			return
		}
	}
	rel := block.GetReliability()
	setBlockToIDBlocks(chain, block.Index, block.Key, rel.HashPower)
	if lost {
		rel.Ready = false
	} else {
		rel.Ready = true
	}
	core.SaveBlockReliability(chain, block.Key[:], rel)
	ch := core.GetChainHeight(chain, block.Key[:])
	ch.Height++
	ch.HashPower += getHashPower(block.Key[:])
	core.SaveChainHeight(chain, block.Previous[:], ch)
	log.Printf("receive new block,chain:%d,index:%d,key:%x,hashpower:%d\n", chain, block.Index, block.Key, rel.HashPower)

	go processEvent(chain)

	if block.Time+2000*1000 < now {
		ctx.Reply(&messages.ReqBlockInfo{Chain: chain, Index: block.Index + 10})
	}

	return nil
}

func processTransaction(chain uint64, key, data []byte) error {
	if chain == 0 {
		return errors.New("not support")
	}

	trans := core.DecodeTrans(data)
	if trans == nil {
		return errors.New("error transaction")
	}
	if bytes.Compare(trans.Key[:], key) != 0 {
		return errors.New("error transaction key")
	}

	nIndex := core.GetLastBlockIndex(chain)
	if trans.Chain > 1 {
		if nIndex <= 1 {
			return errors.New("chain no exist")
		}
	}

	if trans.Chain != chain {
		if trans.Chain != 0 {
			return errors.New("different chain,the Chain of trans must be 0")
		}

		if chain == 1 {
			if nIndex > 1 {
				return errors.New("exist first transaction")
			}
		} else if !core.IsExistTransaction(chain/2, key) {
			return errors.New("different first transaction")
		}
	}

	// future trans
	if trans.Time > uint64(time.Now().Unix())*1000+60000 {
		return errors.New("error time")
	}

	// too old
	if trans.Time+10*24*3600*1000 < core.GetBlockTime(chain) {
		return errors.New("error time")
	}

	coin := core.GetUserCoin(chain, trans.User[:])
	if trans.Chain > 0 && coin < trans.Energy+trans.Cost {
		log.Printf("trans. not enough coin. key:%x,cost:%d\n", trans.Key, coin)
		return errors.New("not enough coin")
	}

	err := core.WriteTransaction(chain, data)
	if err != nil {
		return err
	}

	c := conf.GetConf()
	if (chain == c.ChainOfMine || c.ChainOfMine == 0) && trans.Energy > c.EnergyLimitOfMine {
		addTrans(key, trans.User[:], chain, trans.Time, trans.Energy, uint64(len(data)))
	}

	log.Printf("receive new transaction:%d, %x \n", chain, key)

	go processEvent(chain)
	m := &messages.TransactionInfo{Chain: chain, Key: key}
	network.SendInternalMsg(&messages.BaseMsg{Type: messages.BroadcastMsg, Msg: m})

	return nil
}

func dbRollBack(chain, index uint64, key []byte) error {
	var err error
	nIndex := core.GetLastBlockIndex(chain)
	if nIndex < index {
		return nil
	}
	if nIndex > index+100 {
		return fmt.Errorf("the index < (lastIndex -100),will rollback:%d,last index:%d", index, nIndex)
	}

	lKey := core.GetTheBlockKey(chain, index)
	if bytes.Compare(lKey, key) != 0 {
		return errors.New("error block key of the index")
	}
	for nIndex >= index {
		lKey = core.GetTheBlockKey(chain, nIndex)
		err = database.Rollback(chain, lKey)
		log.Printf("dbRollBack,chain:%d,index:%d,key:%x\n", chain, nIndex, lKey)
		if err != nil {
			log.Println("fail to Rollback.", nIndex, err)
			return err
		}
		stat := core.ReadBlockRunStat(chain, lKey)
		stat.RollbackCount++
		core.SaveBlockRunStat(chain, lKey, stat)
		// core.DeleteBlockReliability(chain, lKey)
		nIndex--
	}

	return nil
}
